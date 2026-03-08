package strategies

import (
	"context"
	"fmt"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/internal/database"
	"github.com/djbro/oracle-weather/internal/resolvers"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
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

	// Oracle lag strategy: we know the outcome from IEM data, so always hold to resolution.
	// The payout at resolution is $1.00/share — selling early at 75-80¢ leaves money on the table.
	currentPrice := pc.getCurrentPrice(*market, pos.Outcome)
	positionAge := time.Since(pos.EntryTime)
	utils.Logger.Debugf("Position %s (%s): Entry=$%.2f, Current=$%.2f, Age=%.1fh — holding to resolution",
		pos.MarketQuestion, pos.Outcome, pos.EntryPrice, currentPrice, positionAge.Hours())

	return false, currentPrice, fmt.Sprintf("holding to resolution (age: %.1fh)", positionAge.Hours())
}

// closePosition sells the position and records the trade
func (pc *PositionCloser) closePosition(pos database.Position, currentPrice float64, reason string) error {
	utils.Logger.Infof("💰 Closing position: %s (%s) - %s", pos.MarketQuestion, pos.Outcome, reason)

	shares := pos.PositionSize / pos.EntryPrice
	var sellAmount float64
	var profit float64
	var profitPercent float64

	// If market is resolved (price=0), redemption happens on-chain automatically.
	// Don't place a sell order — just record the outcome at $1.00 per winning share.
	if currentPrice == 0 {
		utils.Logger.Infof("🏁 Market resolved — skipping sell order, recording redemption at $1.00/share")
		sellAmount = shares * 1.0
		profit = sellAmount - pos.PositionSize
		profitPercent = (profit / pos.PositionSize) * 100
	} else {
		// Check if we have TokenID
		if pos.TokenID == "" {
			utils.Logger.Warnf("⚠️ Cannot sell position - TokenID not stored. Skipping...")
			return fmt.Errorf("no token ID for position")
		}

		sellAmount = shares * currentPrice
		err := pc.client.PlaceSellOrder(pos.TokenID, currentPrice, sellAmount)
		if err != nil {
			return fmt.Errorf("failed to place sell order: %w", err)
		}
		utils.Logger.Infof("✅ Sell order placed: %.2f shares @ $%.2f = $%.2f", shares, currentPrice, sellAmount)
		profit = sellAmount - pos.PositionSize
		profitPercent = (profit / pos.PositionSize) * 100
	}

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

// ValidateAndExitBadPositions checks every open position against the IEM resolver.
// If the resolver now disagrees with the held outcome (or says it's too early to know),
// the position is sold immediately at the best available price.
// This catches positions placed by old/buggy code before peak-hour gating was enforced.
func (pc *PositionCloser) ValidateAndExitBadPositions(ctx context.Context) error {
	utils.Logger.Info("🔎 Validating open positions against IEM resolver...")

	positions, err := database.GetOpenPositions(pc.db)
	if err != nil {
		return fmt.Errorf("failed to get open positions: %w", err)
	}
	if len(positions) == 0 {
		utils.Logger.Info("No open positions to validate")
		return nil
	}

	resolver := resolvers.NewIEMWeatherResolver(pc.config)
	exited := 0

	for _, pos := range positions {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Fetch fresh market data
		market, err := pc.client.GetMarketByID(pos.MarketID)
		if err != nil {
			utils.Logger.Warnf("ValidatePositions: cannot fetch market %s: %v", pos.MarketID, err)
			continue
		}
		if market.Closed {
			utils.Logger.Infof("✅ Market already closed, skipping validation: %s", pos.MarketQuestion)
			continue
		}

		// Ask the IEM resolver what it thinks RIGHT NOW
		outcome, confidence, err := resolver.CheckResolution(*market)
		if err != nil {
			utils.Logger.Warnf("ValidatePositions: resolver error for %q: %v — keeping position", pos.MarketQuestion, err)
			continue
		}

		var exitReason string
		shouldExit := false

		if outcome == nil {
			// Resolver says it's too early to know (pre-peak) — position was placed prematurely
			exitReason = "placed before peak hour — resolver cannot confirm outcome yet"
			shouldExit = true
		} else if *outcome != pos.Outcome {
			// Resolver now disagrees with our position
			exitReason = fmt.Sprintf("resolver now says %s (we hold %s, confidence=%.0f%%)", *outcome, pos.Outcome, confidence*100)
			shouldExit = true
		}

		if !shouldExit {
			utils.Logger.Infof("✅ Position valid: %q → %s confirmed by resolver", pos.MarketQuestion, pos.Outcome)
			continue
		}

		utils.Logger.Warnf("⚠️  BAD POSITION DETECTED: %q (held=%s) — %s", pos.MarketQuestion, pos.Outcome, exitReason)

		// Sell at current market price
		currentPrice := pc.getCurrentPrice(*market, pos.Outcome)
		if currentPrice <= 0 {
			utils.Logger.Warnf("Cannot exit %q — no valid sell price available", pos.MarketQuestion)
			continue
		}
		if pos.TokenID == "" {
			utils.Logger.Warnf("Cannot exit %q — no TokenID stored", pos.MarketQuestion)
			continue
		}

		shares := pos.PositionSize / pos.EntryPrice
		sellAmount := shares * currentPrice
		if err := pc.client.PlaceSellOrder(pos.TokenID, currentPrice, sellAmount); err != nil {
			utils.Logger.Errorf("Failed to exit bad position %q: %v", pos.MarketQuestion, err)
			continue
		}

		profit := sellAmount - pos.PositionSize
		utils.Logger.Infof("🚪 Exited bad position: %q | entry=$%.2f exit=$%.2f | P&L=$%.2f",
			pos.MarketQuestion, pos.EntryPrice, currentPrice, profit)

		// Record closure
		trade := database.Trade{
			MarketID:       pos.MarketID,
			MarketQuestion: pos.MarketQuestion,
			Outcome:        pos.Outcome,
			EntryPrice:     pos.EntryPrice,
			ExitPrice:      currentPrice,
			Profit:         profit,
			Status:         "closed",
		}
		database.LogTrade(pc.db, trade)
		database.ClaimPosition(pc.db, pos.ID, currentPrice)

		exited++
	}

	if exited > 0 {
		utils.Logger.Warnf("⚠️  Exited %d bad positions", exited)
	} else {
		utils.Logger.Info("✅ All open positions validated — none need exiting")
	}
	return nil
}
