package strategies

import (
	"context"
	"fmt"
	"time"

	"github.com/djbro/polymarket-oracle-bot/internal/config"
	"github.com/djbro/polymarket-oracle-bot/internal/database"
	"github.com/djbro/polymarket-oracle-bot/pkg/polymarket"
	"github.com/djbro/polymarket-oracle-bot/pkg/utils"
	bolt "go.etcd.io/bbolt"
)

// PositionCloser automatically closes positions when profitable or resolved
type PositionCloser struct {
	client *polymarket.PolymarketClient
	db     *bolt.DB
	config *config.Config
}

// NewPositionCloser creates a new position closer
func NewPositionCloser(client *polymarket.PolymarketClient, db *bolt.DB, cfg *config.Config) *PositionCloser {
	return &PositionCloser{
		client: client,
		db:     db,
		config: cfg,
	}
}

// CheckAndClosePositions scans all open positions and closes profitable ones
func (pc *PositionCloser) CheckAndClosePositions(ctx context.Context) error {
	utils.Logger.Info("🔍 Checking open positions for closure opportunities...")

	// Get all open positions from database
	positions, err := database.GetOpenPositions(pc.db)
	if err != nil {
		return fmt.Errorf("failed to get open positions: %w", err)
	}

	if len(positions) == 0 {
		utils.Logger.Debug("No open positions to check")
		return nil
	}

	utils.Logger.Infof("Found %d open positions to check", len(positions))

	closedCount := 0
	for _, pos := range positions {
		// Check if context is canceled
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if position should be closed
		shouldClose, currentPrice, reason := pc.shouldClosePosition(pos)
		if !shouldClose {
			utils.Logger.Debugf("Position %s: %s - keeping open (%s)",
				pos.MarketQuestion, pos.Outcome, reason)
			continue
		}

		// Close the position
		if err := pc.closePosition(pos, currentPrice, reason); err != nil {
			utils.Logger.Errorf("Failed to close position %s: %v", pos.MarketQuestion, err)
			continue
		}

		closedCount++
	}

	if closedCount > 0 {
		utils.Logger.Infof("✅ Closed %d positions", closedCount)
	}

	return nil
}

// shouldClosePosition determines if a position should be closed
func (pc *PositionCloser) shouldClosePosition(pos database.Position) (bool, float64, string) {
	// Get current market data
	market, err := pc.client.GetMarketByID(pos.MarketID)
	if err != nil {
		return false, 0, fmt.Sprintf("failed to get market: %v", err)
	}

	// Check if market is closed/resolved
	if market.Closed {
		return true, 0, "market closed/resolved"
	}

	// Get current price for the position's outcome
	currentPrice := pc.getCurrentPrice(*market, pos.Outcome)
	if currentPrice <= 0 {
		return false, 0, "invalid current price"
	}

	// Calculate current profit
	// If we bought at $0.30, current price is $0.90, profit = (0.90 - 0.30) * shares
	profitPerShare := currentPrice - pos.EntryPrice
	profitPercent := (profitPerShare / pos.EntryPrice) * 100

	utils.Logger.Debugf("Position %s (%s): Entry=$%.2f, Current=$%.2f, Profit=%.2f%%",
		pos.MarketQuestion, pos.Outcome, pos.EntryPrice, currentPrice, profitPercent)

	// Close if profit target reached (e.g., 20% profit)
	profitTarget := 20.0 // 20% profit target
	if profitPercent >= profitTarget {
		return true, currentPrice, fmt.Sprintf("profit target reached: %.2f%% (target: %.0f%%)", profitPercent, profitTarget)
	}

	// Close if position is very old and at least break-even (been open > 24 hours)
	positionAge := time.Since(pos.EntryTime)
	if positionAge > 24*time.Hour && profitPercent >= 0 {
		return true, currentPrice, fmt.Sprintf("position aged out: %.1f hours old", positionAge.Hours())
	}

	// Close if market is about to close (within 1 hour of close time)
	if !market.ResolutionTimestamp.IsZero() {
		timeUntilClose := time.Until(market.ResolutionTimestamp)
		if timeUntilClose < time.Hour && profitPercent > -5 {
			return true, currentPrice, fmt.Sprintf("market closing soon: %.0f minutes", timeUntilClose.Minutes())
		}
	}

	return false, currentPrice, fmt.Sprintf("holding (profit: %.2f%%, age: %.1fh)", profitPercent, positionAge.Hours())
}

// closePosition sells the position and records the trade
func (pc *PositionCloser) closePosition(pos database.Position, currentPrice float64, reason string) error {
	utils.Logger.Infof("💰 Closing position: %s (%s) - %s", pos.MarketQuestion, pos.Outcome, reason)

	// Calculate shares and sell amount
	shares := pos.PositionSize / pos.EntryPrice
	sellAmount := shares * currentPrice // Dollar amount we'll receive

	// Check if we have TokenID
	if pos.TokenID == "" {
		utils.Logger.Warnf("⚠️ Cannot sell position - TokenID not stored. Skipping...")
		return fmt.Errorf("no token ID for position")
	}

	// Place sell order
	err := pc.client.PlaceSellOrder(pos.TokenID, currentPrice, sellAmount)
	if err != nil {
		return fmt.Errorf("failed to place sell order: %w", err)
	}

	utils.Logger.Infof("✅ Sell order placed: %.2f shares @ $%.2f = $%.2f", shares, currentPrice, sellAmount)

	// Calculate profit
	profit := sellAmount - pos.PositionSize
	profitPercent := (profit / pos.PositionSize) * 100

	// Log trade to database
	trade := database.Trade{
		MarketID:       pos.MarketID,
		MarketQuestion: pos.MarketQuestion,
		Outcome:        pos.Outcome,
		EntryPrice:     pos.EntryPrice,
		ExitPrice:      currentPrice,
		Profit:         profit,
		Status:         "closed",
	}

	if err := database.LogTrade(pc.db, trade); err != nil {
		utils.Logger.Errorf("Failed to log trade: %v", err)
	}

	// Mark position as claimed (closed)
	if err := database.ClaimPosition(pc.db, pos.ID, currentPrice); err != nil {
		utils.Logger.Errorf("Failed to mark position as closed: %v", err)
	}

	utils.Logger.Infof("📈 Position would close with %.2f%% profit ($%.2f)", profitPercent, profit)

	return nil
}

// getCurrentPrice gets the current price for a specific outcome
func (pc *PositionCloser) getCurrentPrice(market polymarket.Market, outcome string) float64 {
	// Find the index of the outcome
	for i, o := range market.Outcomes {
		if o == outcome {
			// Return the price for this outcome
			if i < len(market.Prices) && market.Prices[i] > 0 {
				return market.Prices[i]
			}
		}
	}
	return 0
}
