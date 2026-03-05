package resolvers

import (
	"time"

	"github.com/djbro/oracle-weather/pkg/polymarket"
)

// Resolver is the interface that all market resolvers must implement
type Resolver interface {
	// CheckResolution checks if a market can be resolved and returns the outcome with confidence
	// Returns (outcome, confidence, error)
	// - outcome: "YES" or "NO" or nil if not resolvable yet
	// - confidence: 0.0 to 1.0 (1.0 = 100% certain)
	// - error: any errors encountered
	CheckResolution(market polymarket.Market) (*string, float64, error)

	// ParseMarketQuestion extracts structured data from a market question
	ParseMarketQuestion(question string) (*MarketData, error)

	// GetConfidence returns the base confidence level (0.0 to 1.0) for this resolver type
	GetConfidence() float64
}

// MarketData represents parsed market question data
type MarketData struct {
	Category    string                 // "weather", "crypto", "sports", etc.
	Location    string                 // For weather/sports
	Date        time.Time              // Resolution date
	Condition   string                 // Weather condition or event type
	Threshold   float64                // Price threshold for crypto
	Asset       string                 // Cryptocurrency or asset name
	Teams       []string               // For sports
	Sport       string                 // Sport type (NBA, NFL, etc.)
	Extra       map[string]interface{} // Additional parsed data
}

// BaseResolver provides common functionality for all resolvers
type BaseResolver struct {
	confidence float64
}

// GetConfidence returns the confidence level
func (b *BaseResolver) GetConfidence() float64 {
	return b.confidence
}

// SetConfidence sets the confidence level
func (b *BaseResolver) SetConfidence(confidence float64) {
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	b.confidence = confidence
}
