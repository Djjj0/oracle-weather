package parity_arbitrage

import "time"

// ArbitrageType represents the type of parity arbitrage
type ArbitrageType string

const (
	// LongArbitrage means buy both YES and NO when sum < 1.00
	LongArbitrage ArbitrageType = "LONG"
	// ShortArbitrage means sell both YES and NO when sum > 1.00
	ShortArbitrage ArbitrageType = "SHORT"
)

// ParityOpportunity represents a parity arbitrage opportunity
type ParityOpportunity struct {
	MarketID           string
	ConditionID        string
	Question           string
	YesTokenID         string  // ERC1155 token ID for YES outcome
	NoTokenID          string  // ERC1155 token ID for NO outcome
	YesPrice           float64
	NoPrice            float64
	PriceSum           float64
	Type               ArbitrageType
	NetProfitPerDollar float64 // Profit per $1 invested
	ExpectedProfit     float64 // For configured position size
	Liquidity          float64
	Volume24h          float64
	DetectedAt         time.Time
	PositionSize       float64 // Calculated position size for this opportunity
}

// ProfitCalculation contains detailed profit breakdown
type ProfitCalculation struct {
	CostToEnter    float64
	ExpectedPayout float64
	TradingFees    float64
	GasFees        float64
	NetProfit      float64
	ProfitPercent  float64
	Profitable     bool
}

// ExecutionResult represents the result of executing a parity trade
type ExecutionResult struct {
	Opportunity      ParityOpportunity
	Success          bool
	YesOrderFilled   bool
	NoOrderFilled    bool
	ActualYesPrice   float64
	ActualNoPrice    float64
	FeesPaid         float64
	ExecutedAt       time.Time
	Error            error
}
