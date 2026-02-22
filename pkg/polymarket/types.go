package polymarket

import (
	"time"

	"github.com/djbro/oracle-weather/internal/config"
)

// Market represents a Polymarket prediction market
type Market struct {
	ID                  string    `json:"id"`
	Question            string    `json:"question"`
	Description         string    `json:"description"`
	Category            string    `json:"category"`
	Outcomes            []string  `json:"-"`                 // Populated from outcomes string
	OutcomesStr         string    `json:"outcomes"`          // Gamma returns as string
	Active              bool      `json:"active"`
	Closed              bool      `json:"closed"`
	ResolutionTimestamp time.Time `json:"endDate"`           // Gamma API uses "endDate"
	EndDateIso          string    `json:"endDateIso"`        // Gamma API date string
	ResolutionDate      string    `json:"resolution_date"`   // Legacy field
	ResolvedOutcome     *string   `json:"resolved_outcome"`
	Volume              float64   `json:"volume,string"`     // Gamma returns as string
	Liquidity           float64   `json:"liquidityNum"`      // Gamma has numeric version
	TokenIDs            []string  `json:"-"`                 // Populated from clobTokenIds string
	TokenIDsStr         string    `json:"clobTokenIds"`      // Gamma returns as string
	Prices              []float64 `json:"-"`                 // Populated from outcomePrices string
	OutcomePricesStr    string    `json:"outcomePrices"`     // Gamma returns as string
	CreatedAt           time.Time `json:"createdAt"`         // Gamma uses camelCase
	UpdatedAt           time.Time `json:"updatedAt"`         // Gamma uses camelCase
	FetchedAt           time.Time `json:"-"`                 // Track when we fetched this data
}

// IsStale checks if market data is stale (older than 60 seconds)
func (m *Market) IsStale() bool {
	if m.FetchedAt.IsZero() {
		return false // No fetch time set, assume fresh
	}
	return time.Since(m.FetchedAt) > 60*time.Second
}

// Orderbook represents the order book for a market
type Orderbook struct {
	MarketID string  `json:"market_id"`
	Outcome  string  `json:"outcome"`
	Bids     []Order `json:"bids"`
	Asks     []Order `json:"asks"`
}

// Order represents a single order in the orderbook
type Order struct {
	Price    float64 `json:"price"`
	Size     float64 `json:"size"`
	OrderID  string  `json:"order_id"`
	MakerID  string  `json:"maker_id"`
	Side     string  `json:"side"` // "BUY" or "SELL"
}

// Token represents a market outcome token
type Token struct {
	ID       string  `json:"id"`
	TokenID  string  `json:"token_id"`
	MarketID string  `json:"market_id"`
	Outcome  string  `json:"outcome"`
	Price    float64 `json:"price"`
	Winner   bool    `json:"winner"`
}

// Balance represents user's balance
type Balance struct {
	USD      float64            `json:"usd"`
	Tokens   map[string]float64 `json:"tokens"`
}

// TradeRequest represents a request to place a trade
type TradeRequest struct {
	MarketID string  `json:"market_id"`
	Outcome  string  `json:"outcome"`
	Side     string  `json:"side"` // "BUY" or "SELL"
	Size     float64 `json:"size"`
	Price    float64 `json:"price,omitempty"` // For limit orders
}

// TradeResponse represents the response from placing a trade
type TradeResponse struct {
	OrderID   string    `json:"order_id"`
	Status    string    `json:"status"`
	Filled    float64   `json:"filled"`
	Remaining float64   `json:"remaining"`
	Timestamp time.Time `json:"timestamp"`
}

// MarketsResponse represents the API response for markets list
type MarketsResponse struct {
	Markets []Market `json:"data"` // Polymarket API uses "data" field
	Count   int      `json:"count"`
}

// PolymarketClient is an alias for the lixvyang SDK client (WORKING!)
// Switched from PolymarketClientOfficial to fix authentication issues
type PolymarketClient = PolymarketClientLixv

// NewClient is an alias to NewClientLixv (using working SDK)
// This maintains backward compatibility with existing code
func NewClient(cfg *config.Config) *PolymarketClient {
	return NewClientLixv(cfg)
}
