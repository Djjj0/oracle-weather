package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
)

// MarketMonitor monitors Polymarket for arbitrage opportunities
type MarketMonitor struct {
	client           *polymarket.PolymarketClient
	config           *config.Config
	categoryKeywords map[string][]string // Pre-compiled keywords for faster matching
}

// NewMonitor creates a new market monitor
func NewMonitor(client *polymarket.PolymarketClient, config *config.Config) *MarketMonitor {
	// PHASE 7 OPTIMIZATION: Pre-compile category keywords for faster matching
	categoryKeywords := map[string][]string{
		"weather": {"rain", "temperature", "snow", "weather", "sunny", "cloudy", "storm"},
		"crypto":  {"btc", "bitcoin", "eth", "ethereum", "crypto", "sol", "solana", "ada", "cardano"},
		"sports":  {"beat", "win", "score", "game", "match", "lakers", "celtics", "patriots", "nba", "nfl"},
	}

	return &MarketMonitor{
		client:           client,
		config:           config,
		categoryKeywords: categoryKeywords,
	}
}

// EarlyEntryWindow is how far ahead of resolution we allow entry when the IEM
// resolver already has a confirmed outcome (e.g. running high already exceeds
// the threshold, so resolution is a foregone conclusion).
const EarlyEntryWindow = 2 * time.Hour

// GetMarketsPastResolution returns weather markets that are eligible for trading:
//   - Markets where resolution time has already passed (classic oracle-lag), OR
//   - Markets resolving within the next EarlyEntryWindow (2 h) where the IEM
//     resolver may already have a confirmed outcome based on intraday data.
func (m *MarketMonitor) GetMarketsPastResolution(ctx context.Context) ([]polymarket.Market, error) {
	utils.Logger.Info("Fetching active markets...")

	// Get all active markets
	markets, err := m.client.GetActiveMarkets()
	if err != nil {
		return nil, fmt.Errorf("failed to get active markets: %w", err)
	}

	utils.Logger.Infof("Found %d active markets", len(markets))

	// Filter markets past resolution time OR resolving within EarlyEntryWindow
	var pastResolution []polymarket.Market
	now := time.Now()
	earlyEntryDeadline := now.Add(EarlyEntryWindow)

	for _, market := range markets {
		// Skip if not active or already resolved
		if !market.Active || market.Closed || market.ResolvedOutcome != nil {
			continue
		}

		// ONLY scan weather markets (we haven't implemented crypto/sports yet)
		category := m.CategorizeMarket(market)
		if category != "weather" {
			continue
		}

		if market.ResolutionTimestamp.IsZero() {
			continue
		}

		// Include markets past resolution time (oracle-lag) OR resolving soon
		// (early entry when IEM already has a confirmed intraday outcome).
		pastResolutionTime := now.After(market.ResolutionTimestamp)
		resolvingSoon := market.ResolutionTimestamp.Before(earlyEntryDeadline)

		if pastResolutionTime || resolvingSoon {
			pastResolution = append(pastResolution, market)
		}
	}

	pastCount := 0
	earlyCount := 0
	for _, m := range pastResolution {
		if now.After(m.ResolutionTimestamp) {
			pastCount++
		} else {
			earlyCount++
		}
	}
	utils.Logger.Infof("Found %d weather markets eligible (past resolution: %d, early entry within %v: %d)",
		len(pastResolution), pastCount, EarlyEntryWindow, earlyCount)

	return pastResolution, nil
}

// GetMarketsPastResolutionConcurrent scans markets concurrently
func (m *MarketMonitor) GetMarketsPastResolutionConcurrent(ctx context.Context) (<-chan polymarket.Market, <-chan error) {
	marketsChan := make(chan polymarket.Market, 100)
	errorsChan := make(chan error, 1)

	go func() {
		defer close(marketsChan)
		defer close(errorsChan)

		markets, err := m.GetMarketsPastResolution(ctx)
		if err != nil {
			errorsChan <- err
			return
		}

		// Send markets to channel
		for _, market := range markets {
			select {
			case <-ctx.Done():
				return
			case marketsChan <- market:
			}
		}
	}()

	return marketsChan, errorsChan
}

// CategorizeMarket determines the category from the question text
// PHASE 7 OPTIMIZATION: Uses pre-compiled keywords for faster matching
func (m *MarketMonitor) CategorizeMarket(market polymarket.Market) string {
	// Use existing category if available
	if market.Category != "" {
		return market.Category
	}

	// Auto-detect from question (lowercase once for efficiency)
	question := strings.ToLower(market.Question)

	// Check each category's keywords (order matters - check more specific first)
	for category, keywords := range m.categoryKeywords {
		for _, keyword := range keywords {
			if strings.Contains(question, keyword) {
				return category
			}
		}
	}

	return "unknown"
}

// CalculateLag calculates how long past resolution the market is
func (m *MarketMonitor) CalculateLag(market polymarket.Market) time.Duration {
	if market.ResolutionTimestamp.IsZero() {
		return 0
	}

	return time.Since(market.ResolutionTimestamp)
}

// ScanMarketsParallel scans multiple markets in parallel
func (m *MarketMonitor) ScanMarketsParallel(ctx context.Context, markets []polymarket.Market, workerCount int) <-chan ScanResult {
	results := make(chan ScanResult, len(markets))
	marketsChan := make(chan polymarket.Market, len(markets))

	// Send markets to channel
	go func() {
		for _, market := range markets {
			select {
			case <-ctx.Done():
				close(marketsChan)
				return
			case marketsChan <- market:
			}
		}
		close(marketsChan)
	}()

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for market := range marketsChan {
				result := ScanResult{
					Market:   market,
					Category: m.CategorizeMarket(market),
					Lag:      m.CalculateLag(market),
				}
				select {
				case <-ctx.Done():
					return
				case results <- result:
				}
			}
		}()
	}

	// Close results when all workers done
	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

// ScanResult represents the result of scanning a market
type ScanResult struct {
	Market   polymarket.Market
	Category string
	Lag      time.Duration
	Error    error
}
