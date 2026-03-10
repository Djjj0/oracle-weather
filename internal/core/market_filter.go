package core

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
)

// MarketFilter filters out low-quality markets
type MarketFilter struct {
	config      *config.Config
	blacklist   map[string]bool
	badPatterns []string
	mu          sync.RWMutex
}

// NewMarketFilter creates a new market filter
func NewMarketFilter(cfg *config.Config) *MarketFilter {
	return &MarketFilter{
		config:      cfg,
		blacklist:   make(map[string]bool),
		badPatterns: defaultBadPatterns(),
	}
}

// ShouldSkip checks if a market should be filtered out
// Returns (shouldSkip bool, reason string)
func (mf *MarketFilter) ShouldSkip(market polymarket.Market) (bool, string) {
	// Check blacklist first
	if mf.isBlacklisted(market.ID) {
		return true, "blacklisted market"
	}

	// Check liquidity
	if market.Liquidity < mf.config.MinLiquidity {
		return true, fmt.Sprintf("low liquidity ($%.2f < $%.2f)", market.Liquidity, mf.config.MinLiquidity)
	}

	// Check volume
	if market.Volume < mf.config.MinVolume {
		return true, fmt.Sprintf("low volume ($%.2f < $%.2f)", market.Volume, mf.config.MinVolume)
	}

	// Check market age (must be at least X hours old)
	if !market.CreatedAt.IsZero() {
		age := time.Since(market.CreatedAt)
		minAge := time.Duration(mf.config.MinMarketAge) * time.Hour
		if age < minAge {
			return true, fmt.Sprintf("market too new (%.1f hours < %d hours)", age.Hours(), mf.config.MinMarketAge)
		}
	}

	// Check spread (if we have prices)
	if len(market.Prices) >= 2 {
		spread := calculateSpread(market.Prices)
		if spread > mf.config.MaxSpread {
			return true, fmt.Sprintf("spread too wide (%.1f%% > %.1f%%)", spread*100, mf.config.MaxSpread*100)
		}
	}

	// Check for ambiguous question patterns
	question := strings.ToLower(market.Question)
	for _, pattern := range mf.badPatterns {
		if strings.Contains(question, pattern) {
			return true, fmt.Sprintf("ambiguous question contains: '%s'", pattern)
		}
	}

	// Check if market is already closed
	if market.Closed {
		return true, "market already closed"
	}

	return false, ""
}

// AddToBlacklist adds a market ID to the blacklist
func (mf *MarketFilter) AddToBlacklist(marketID string) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.blacklist[marketID] = true
}

// RemoveFromBlacklist removes a market ID from the blacklist
func (mf *MarketFilter) RemoveFromBlacklist(marketID string) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	delete(mf.blacklist, marketID)
}

// isBlacklisted checks if a market is blacklisted
func (mf *MarketFilter) isBlacklisted(marketID string) bool {
	mf.mu.RLock()
	defer mf.mu.RUnlock()
	return mf.blacklist[marketID]
}

// GetBlacklistSize returns the number of blacklisted markets
func (mf *MarketFilter) GetBlacklistSize() int {
	mf.mu.RLock()
	defer mf.mu.RUnlock()
	return len(mf.blacklist)
}

// calculateSpread calculates the parity drift for binary markets.
// NOTE: This is NOT a true bid/ask spread. It measures how far YES+NO prices
// deviate from the expected sum of 1.0 (parity check). A large deviation
// indicates an illiquid or mis-priced book. Rename candidate: "calculateParityDrift".
func calculateSpread(prices []float64) float64 {
	if len(prices) < 2 {
		return 0
	}

	// In binary markets: YES price + NO price should = 1.0
	// Parity drift = |1.0 - (YES + NO)|
	sum := 0.0
	for _, price := range prices {
		sum += price
	}

	drift := 1.0 - sum
	if drift < 0 {
		drift = -drift
	}

	return drift
}

// defaultBadPatterns returns question patterns to avoid
func defaultBadPatterns() []string {
	return []string{
		// Subjective/vague
		"will elon tweet",
		"will elon post",
		"will elon musk tweet",
		"will x post",
		"will trump tweet",
		"will someone announce",
		"approximately",
		"around",
		"roughly",
		"about",

		// Timing ambiguity
		"by end of year",
		"by eoy",
		"before 2026",
		"sometime in",

		// Measurement ambiguity
		"substantially",
		"significantly",
		"major",
		"massive",

		// Source ambiguity
		"according to",
		"reports say",
		"rumors",

		// Complex conditionals
		"if and only if",
		"provided that",
		"assuming",
	}
}
