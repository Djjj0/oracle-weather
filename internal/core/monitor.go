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

// GetMarketsPastResolution returns markets where resolution time has passed but still ACTIVE
func (m *MarketMonitor) GetMarketsPastResolution(ctx context.Context) ([]polymarket.Market, error) {
	utils.Logger.Info("Fetching active markets...")

	// Get all active markets
	markets, err := m.client.GetActiveMarkets()
	if err != nil {
		return nil, fmt.Errorf("failed to get active markets: %w", err)
	}

	utils.Logger.Infof("Found %d active markets", len(markets))

	// Filter markets past resolution time
	var pastResolution []polymarket.Market
	now := time.Now()

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

		// Check if resolution time has passed
		if !market.ResolutionTimestamp.IsZero() && now.After(market.ResolutionTimestamp) {
			pastResolution = append(pastResolution, market)
		}
	}

	utils.Logger.Infof("Found %d weather markets past resolution time", len(pastResolution))

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

	// POSITIVE WEATHER PATTERN MATCHING - only match very specific weather questions
	isWeather := false

	// Pattern 1: Temperature questions with degrees/temperature
	if (strings.Contains(question, "temperature") || strings.Contains(question, "degrees") ||
		strings.Contains(question, "°c") || strings.Contains(question, "°f")) &&
		!strings.Contains(question, "global temperature") { // Exclude climate change markets for now
		// Check for city names (common weather markets)
		cities := []string{"london", "new york", "chicago", "los angeles", "seattle", "miami",
			"paris", "tokyo", "sydney", "beijing", "delhi", "moscow"}
		for _, city := range cities {
			if strings.Contains(question, city) {
				isWeather = true
				break
			}
		}
	}

	// Pattern 2: Hurricane/storm formation (not team names)
	if !isWeather {
		if (strings.Contains(question, "hurricane") || strings.Contains(question, "tropical storm")) &&
			(strings.Contains(question, "landfall") || strings.Contains(question, "form") ||
				strings.Contains(question, "category")) &&
			!strings.Contains(question, "hurricanes") { // Exclude team (plural)
			isWeather = true
		}
	}

	// Pattern 3: Named storms
	if !isWeather {
		if strings.Contains(question, "named storm") {
			isWeather = true
		}
	}

	// Pattern 4: Rain in specific cities
	if !isWeather {
		if (strings.Contains(question, " rain ") || strings.Contains(question, " rain?") ||
			strings.Contains(question, "raining")) {
			// Exclude false positives
			if !strings.Contains(question, "ukraine") && !strings.Contains(question, "train") &&
				!strings.Contains(question, "drain") && !strings.Contains(question, "brain") &&
				!strings.Contains(question, "character of rain") {
				isWeather = true
			}
		}
	}

	// Pattern 5: Snow questions
	if !isWeather {
		if (strings.Contains(question, " snow ") || strings.Contains(question, " snow?") ||
			strings.Contains(question, "snowing")) &&
			!strings.Contains(question, "snowflake") && !strings.Contains(question, "snow ") {
			isWeather = true
		}
	}

	if isWeather {
		return "weather"
	}

	// Check other categories
	for category, keywords := range m.categoryKeywords {
		if category == "weather" {
			continue // Already handled above
		}
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
