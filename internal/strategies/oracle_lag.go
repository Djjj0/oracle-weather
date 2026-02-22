package strategies

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/internal/core"
	"github.com/djbro/oracle-weather/internal/database"
	"github.com/djbro/oracle-weather/internal/resolvers"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	bolt "go.etcd.io/bbolt"
)

const (
	// Polymarket fee is approximately 3.15%
	PolymarketFee = 0.0315
	// Maximum price movement allowed before execution (5%)
	MaxPriceMovement = 0.05
	// Minimum time between re-executing same trade (1 hour)
	MinReExecutionDelay = 1 * time.Hour
	// Cleanup old executed trades after 24 hours
	ExecutedTradesCleanup = 24 * time.Hour
)

// ExecutedTrades tracks recently executed trades to prevent duplicates
type ExecutedTrades struct {
	trades map[string]time.Time // marketID:outcome -> execution time
	mu     sync.RWMutex
}

// NewExecutedTrades creates a new executed trades tracker
func NewExecutedTrades() *ExecutedTrades {
	return &ExecutedTrades{
		trades: make(map[string]time.Time),
	}
}

// AlreadyExecuted checks if a trade was recently executed
func (et *ExecutedTrades) AlreadyExecuted(marketID, outcome string) bool {
	et.mu.RLock()
	defer et.mu.RUnlock()

	key := marketID + ":" + outcome

	if execTime, exists := et.trades[key]; exists {
		// Only allow re-execution if more than 1 hour passed
		if time.Since(execTime) < MinReExecutionDelay {
			return true
		}
	}

	return false
}

// RecordExecution records a trade execution
func (et *ExecutedTrades) RecordExecution(marketID, outcome string) {
	et.mu.Lock()
	defer et.mu.Unlock()

	key := marketID + ":" + outcome
	et.trades[key] = time.Now()

	// Clean up old entries (>24 hours) - prevent memory leak
	for k, t := range et.trades {
		if time.Since(t) > ExecutedTradesCleanup {
			delete(et.trades, k)
		}
	}
}

// Opportunity represents a trading opportunity
type Opportunity struct {
	MarketID       string
	MarketQuestion string
	Outcome        string
	TokenID        string // ERC1155 token ID for order placement
	CurrentPrice   float64
	ExpectedProfit float64
	Edge           float64 // Edge for this trade (true_prob - market_price)
	Confidence     float64
	Lag            time.Duration
	Timestamp      time.Time
}

// TemperatureBucket represents a temperature range bucket
type TemperatureBucket struct {
	City      string
	Date      string
	LowTemp   float64
	HighTemp  float64
	IsRange   bool // true for "48-49°F", false for "50°F or higher"
	MarketID  string
}

// parseBucketFromQuestion extracts temperature bucket info from market question
func parseBucketFromQuestion(question string) *TemperatureBucket {
	// Pattern 1: "Will the highest temperature in [city] be between [X]-[Y]°F on [date]?"
	rangePattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be between (\d+)-(\d+)°?[fc] on ([a-z]+\s+\d+)`)
	if matches := rangePattern.FindStringSubmatch(question); len(matches) > 4 {
		low, _ := strconv.ParseFloat(matches[2], 64)
		high, _ := strconv.ParseFloat(matches[3], 64)
		return &TemperatureBucket{
			City:     strings.TrimSpace(matches[1]),
			Date:     strings.TrimSpace(matches[4]),
			LowTemp:  low,
			HighTemp: high,
			IsRange:  true,
		}
	}

	// Pattern 2: "Will the highest temperature in [city] be [X]°F or higher on [date]?"
	higherPattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be (\d+)°?[fc] or higher on ([a-z]+\s+\d+)`)
	if matches := higherPattern.FindStringSubmatch(question); len(matches) > 3 {
		threshold, _ := strconv.ParseFloat(matches[2], 64)
		return &TemperatureBucket{
			City:     strings.TrimSpace(matches[1]),
			Date:     strings.TrimSpace(matches[3]),
			LowTemp:  threshold,
			HighTemp: 999, // Open-ended high
			IsRange:  false,
		}
	}

	// Pattern 3: "Will the highest temperature in [city] be [X]°F or lower on [date]?"
	lowerPattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be (\d+)°?[fc] or lower on ([a-z]+\s+\d+)`)
	if matches := lowerPattern.FindStringSubmatch(question); len(matches) > 3 {
		threshold, _ := strconv.ParseFloat(matches[2], 64)
		return &TemperatureBucket{
			City:     strings.TrimSpace(matches[1]),
			Date:     strings.TrimSpace(matches[3]),
			LowTemp:  -999, // Open-ended low
			HighTemp: threshold,
			IsRange:  false,
		}
	}

	// Pattern 4: Celsius variants
	celsiusRangePattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be between (-?\d+)-(-?\d+)°?c on ([a-z]+\s+\d+)`)
	if matches := celsiusRangePattern.FindStringSubmatch(question); len(matches) > 4 {
		low, _ := strconv.ParseFloat(matches[2], 64)
		high, _ := strconv.ParseFloat(matches[3], 64)
		return &TemperatureBucket{
			City:     strings.TrimSpace(matches[1]),
			Date:     strings.TrimSpace(matches[4]),
			LowTemp:  low,
			HighTemp: high,
			IsRange:  true,
		}
	}

	return nil
}

// isBucketAdjacent checks if two buckets are adjacent (within 1 degree)
func isBucketAdjacent(bucket1, bucket2 *TemperatureBucket) bool {
	if bucket1 == nil || bucket2 == nil {
		return false
	}

	// Must be same city and date
	if strings.ToLower(bucket1.City) != strings.ToLower(bucket2.City) {
		return false
	}
	if bucket1.Date != bucket2.Date {
		return false
	}

	// Check if ranges are adjacent or overlapping
	// Adjacent means: bucket1.HighTemp + 1 = bucket2.LowTemp OR bucket2.HighTemp + 1 = bucket1.LowTemp
	// Overlapping means: ranges share any temperature value

	// Skip open-ended buckets (X or higher, X or lower)
	if !bucket1.IsRange || !bucket2.IsRange {
		return false
	}

	// Check if adjacent: gap of exactly 1 degree or overlapping
	gap1 := bucket2.LowTemp - bucket1.HighTemp
	gap2 := bucket1.LowTemp - bucket2.HighTemp

	// Adjacent if gap is 0 or 1 (touching or 1 degree apart)
	return (gap1 >= 0 && gap1 <= 1) || (gap2 >= 0 && gap2 <= 1)
}

// getExpectedWinningBucket determines which bucket the actual temperature falls into
func getExpectedWinningBucket(actualTemp float64, buckets []*TemperatureBucket) *TemperatureBucket {
	for _, bucket := range buckets {
		if bucket == nil {
			continue
		}

		// Check if actualTemp falls in this bucket's range
		if actualTemp >= bucket.LowTemp && actualTemp <= bucket.HighTemp {
			return bucket
		}
	}
	return nil
}

// OracleLagStrategy implements oracle lag arbitrage
type OracleLagStrategy struct {
	monitor        *core.MarketMonitor
	factory        *resolvers.Factory
	client         *polymarket.PolymarketClient
	db             *bolt.DB
	config         *config.Config
	circuitBreaker *core.CircuitBreaker
	marketFilter   *core.MarketFilter
	executedTrades *ExecutedTrades
}

// NewStrategy creates a new oracle lag strategy
func NewStrategy(
	monitor *core.MarketMonitor,
	factory *resolvers.Factory,
	client *polymarket.PolymarketClient,
	db *bolt.DB,
	config *config.Config,
) *OracleLagStrategy {
	circuitBreaker := core.NewCircuitBreaker(config.DailyLossLimit, config.DailyTradeLimit)
	marketFilter := core.NewMarketFilter(config)
	executedTrades := NewExecutedTrades()

	return &OracleLagStrategy{
		monitor:        monitor,
		factory:        factory,
		client:         client,
		db:             db,
		config:         config,
		circuitBreaker: circuitBreaker,
		marketFilter:   marketFilter,
		executedTrades: executedTrades,
	}
}

// ScanOpportunities scans for arbitrage opportunities
func (s *OracleLagStrategy) ScanOpportunities(ctx context.Context) <-chan Opportunity {
	opportunitiesChan := make(chan Opportunity, 10)

	go func() {
		defer close(opportunitiesChan)

		// Check circuit breaker first
		canTrade, reason := s.circuitBreaker.CanTrade()
		if !canTrade {
			utils.Logger.Errorf("🚨 Circuit breaker is tripped: %s", reason)
			utils.Logger.Error("Trading is DISABLED. Reset required.")
			return
		}

		// Get markets past resolution
		utils.Logger.Info("Scanning for opportunities...")
		markets, err := s.monitor.GetMarketsPastResolution(ctx)
		if err != nil {
			utils.Logger.Errorf("Failed to get markets: %v", err)
			return
		}

		utils.Logger.Infof("Checking %d markets for opportunities...", len(markets))

		// Check each market
		for _, market := range markets {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Apply market quality filter
			if shouldSkip, reason := s.marketFilter.ShouldSkip(market); shouldSkip {
				utils.Logger.Debugf("Skipping market %s: %s", market.Question, reason)
				continue
			}

			// Get appropriate resolver
			category := s.monitor.CategorizeMarket(market)
			resolver := s.factory.GetResolver(category)
			if resolver == nil {
				// Try auto-detection from question
				resolver = s.factory.GetResolverByQuestion(market.Question)
				if resolver == nil {
					utils.Logger.Debugf("No resolver found for market: %s", market.Question)
					continue
				}
			}

			// Check if market can be resolved
			outcome, confidence, err := resolver.CheckResolution(market)
			if err != nil {
				utils.Logger.Debugf("Resolution check failed for %s: %v", market.Question, err)
				continue
			}

			if outcome == nil {
				utils.Logger.Debugf("Market not yet resolvable: %s", market.Question)
				continue
			}

			// Skip if confidence is too low (< 66%)
			if confidence < 0.66 {
				utils.Logger.Debugf("Skipping %s: confidence too low (%.1f%%)", market.Question, confidence*100)
				continue
			}

			// Get market prices for both YES and NO outcomes
			yesPrice := s.getCurrentPrice(market, "YES")
			noPrice := s.getCurrentPrice(market, "NO")

			// Validate prices
			if yesPrice <= 0 || noPrice <= 0 {
				utils.Logger.Debugf("Invalid prices for market: %s (YES: $%.2f, NO: $%.2f)", market.Question, yesPrice, noPrice)
				continue
			}

			// Skip dead/stale markets (prices below 5 cents indicate no real trading activity)
			// These markets often have $0.00 or $0.01 prices which create false "opportunities"
			if yesPrice < 0.05 || noPrice < 0.05 {
				utils.Logger.Debugf("Dead market (price < $0.05) skipped: %s (YES: $%.2f, NO: $%.2f)",
					market.Question, yesPrice, noPrice)
				continue
			}

			// Calculate edge for both sides
			// Confidence represents true probability of YES outcome
			// Edge for YES = true_prob_yes - price_yes
			// Edge for NO = true_prob_no - price_no = (1 - true_prob_yes) - (1 - price_yes)
			trueProbYes := confidence
			trueProbNo := 1.0 - confidence

			edgeYes := trueProbYes - yesPrice
			edgeNo := trueProbNo - noPrice

			// Log edge analysis
			utils.Logger.Debugf("📊 Edge Analysis: %s", market.Question)
			utils.Logger.Debugf("  Confidence (true prob YES): %.1f%%", trueProbYes*100)
			utils.Logger.Debugf("  YES: price=$%.3f, edge=%.1f%% | NO: price=$%.3f, edge=%.1f%%",
				yesPrice, edgeYes*100, noPrice, edgeNo*100)

			// Determine which side has better edge
			bestOutcome := *outcome
			bestPrice := yesPrice
			bestEdge := edgeYes
			consideringNo := false

			if edgeNo > edgeYes {
				// NO side has better edge
				if *outcome == "Yes" {
					bestOutcome = "No"
				} else {
					bestOutcome = "Yes"
				}
				bestPrice = noPrice
				bestEdge = edgeNo
				consideringNo = true
				utils.Logger.Debugf("  ✓ Selected NO side (edge: %.1f%% > %.1f%%)", edgeNo*100, edgeYes*100)
			} else {
				utils.Logger.Debugf("  ✓ Selected YES side (edge: %.1f%% > %.1f%%)", edgeYes*100, edgeNo*100)
			}

			// ADJACENT BUCKET EXCLUSION: Skip NO bets on temperature buckets in uncertain zone
			// This prevents betting NO on buckets adjacent to the actual temperature
			if consideringNo {
				bucket := parseBucketFromQuestion(market.Question)
				if bucket != nil && bucket.IsRange {
					// For temperature range buckets, use confidence to detect adjacency risk
					//
					// Confidence interpretation:
					// - High confidence (>90%) in YES → actual temp IS in this bucket → don't bet NO!
					// - Low confidence (<10%) in NO → actual temp is FAR from this bucket → safe to bet NO
					// - Medium confidence (10-90%) → actual temp MIGHT BE NEAR this bucket → SKIP NO bet
					//
					// The "uncertain zone" (15-85%) catches buckets adjacent to actual temp
					// Example: If actual temp is 51°F:
					//   - "50-51°F" bucket: ~95% confidence YES (in the bucket)
					//   - "52-53°F" bucket: ~20% confidence YES (adjacent - SKIP NO)
					//   - "48-49°F" bucket: ~20% confidence YES (adjacent - SKIP NO)
					//   - "54-55°F" bucket: ~5% confidence YES (far away - OK to bet NO)

					if confidence >= 0.15 && confidence <= 0.85 {
						utils.Logger.Infof("  ⚠️  SKIPPING NO bet - bucket in uncertain zone (adjacent risk)")
						utils.Logger.Debugf("    Market: %s", market.Question)
						utils.Logger.Debugf("    Confidence %.1f%% suggests actual temp near %.0f-%.0f range",
							confidence*100, bucket.LowTemp, bucket.HighTemp)
						utils.Logger.Debugf("    Buying NO on adjacent buckets wastes money when temp is between them")
						continue
					}

					utils.Logger.Debugf("  ✓ NO bet approved - bucket far from actual temp (confidence: %.1f%%)", confidence*100)
				}
			}

			// Use dynamic profit threshold (convert to edge threshold)
			dynamicThreshold := s.getDynamicThreshold()
			if bestEdge < dynamicThreshold {
				utils.Logger.Debugf("  ✗ Edge too low: %.2f%% < %.2f%% threshold",
					bestEdge*100, dynamicThreshold*100)
				continue
			}

			// Calculate expected profit based on edge
			// Expected value = edge * position_size (before fees)
			// After fees: EV = (edge - fee) * position_size
			expectedProfit := bestEdge - (bestPrice * PolymarketFee)

			// Find the token ID for the outcome
			tokenID := s.getTokenIDForOutcome(market, bestOutcome)
			if tokenID == "" {
				utils.Logger.Debugf("No token ID found for outcome %s in market %s", bestOutcome, market.Question)
				continue
			}

			// Create opportunity with actual confidence from resolution
			opp := Opportunity{
				MarketID:       market.ID,
				MarketQuestion: market.Question,
				Outcome:        bestOutcome,
				TokenID:        tokenID,
				CurrentPrice:   bestPrice,
				ExpectedProfit: expectedProfit,
				Edge:           bestEdge,
				Confidence:     confidence,
				Lag:            s.monitor.CalculateLag(market),
				Timestamp:      time.Now(),
			}

			utils.Logger.Infof("🎯 OPPORTUNITY FOUND: %s | %s | Price: $%.2f | Edge: %.2f%% | Profit: %.2f%%",
				market.Question, bestOutcome, bestPrice, bestEdge*100, expectedProfit*100)

			// Log to database
			dbOpp := database.Opportunity{
				MarketID:       opp.MarketID,
				Outcome:        opp.Outcome,
				CurrentPrice:   opp.CurrentPrice,
				ExpectedProfit: opp.ExpectedProfit,
				Executed:       false,
			}
			if err := database.LogOpportunity(s.db, dbOpp); err != nil {
				utils.Logger.Errorf("Failed to log opportunity: %v", err)
			}

			// Send to channel
			select {
			case <-ctx.Done():
				return
			case opportunitiesChan <- opp:
			}
		}

		utils.Logger.Info("Opportunity scan complete")
	}()

	return opportunitiesChan
}

// ExecuteOpportunity executes a trading opportunity
func (s *OracleLagStrategy) ExecuteOpportunity(ctx context.Context, opp Opportunity) error {
	// Check circuit breaker before trading
	canTrade, reason := s.circuitBreaker.CanTrade()
	if !canTrade {
		utils.Logger.Errorf("🚨 Cannot execute trade - circuit breaker tripped: %s", reason)
		return fmt.Errorf("circuit breaker tripped: %s", reason)
	}

	// PHASE 7: Check for duplicate trades
	if s.executedTrades.AlreadyExecuted(opp.MarketID, opp.Outcome) {
		utils.Logger.Infof("⏭️  Skipping - already executed this opportunity within last hour")
		return nil
	}

	utils.Logger.Infof("💰 Executing opportunity: %s", opp.MarketQuestion)

	// Validate opportunity still exists - PHASE 7: Fresh data check
	market, err := s.client.GetMarketByID(opp.MarketID)
	if err != nil {
		return fmt.Errorf("failed to get market: %w", err)
	}

	// Check if market is still active
	if market.Closed {
		utils.Logger.Warnf("Market already closed: %s", opp.MarketQuestion)
		return fmt.Errorf("market already closed")
	}

	// PHASE 7: Verify price hasn't moved significantly before execution
	currentPrice := s.getCurrentPrice(*market, opp.Outcome)
	if currentPrice <= 0 {
		return fmt.Errorf("invalid current price")
	}

	// Check if price moved
	priceMovement := math.Abs(currentPrice - opp.CurrentPrice)
	if priceMovement > MaxPriceMovement {
		priceChangePercent := (priceMovement / opp.CurrentPrice) * 100

		// Recalculate profit with new price
		newExpectedProfit := (1.0 - currentPrice - PolymarketFee) * 100

		// If still profitable, use the new price
		if newExpectedProfit >= s.config.MinProfitThreshold*100 {
			utils.Logger.Infof("⚠️  Price moved %.2f%% (%.2f → %.2f) but still profitable (%.2f%%), continuing...",
				priceChangePercent, opp.CurrentPrice, currentPrice, newExpectedProfit)
			opp.CurrentPrice = currentPrice
			opp.ExpectedProfit = newExpectedProfit / 100 // Store as decimal
		} else {
			utils.Logger.Warnf("⚠️  Price moved %.2f%% and no longer profitable: expected profit %.2f%% < threshold %.2f%%",
				priceChangePercent, newExpectedProfit, s.config.MinProfitThreshold*100)
			return fmt.Errorf("price moved and opportunity no longer profitable")
		}
	} else {
		// Price didn't move much, use the fresh price
		opp.CurrentPrice = currentPrice
	}

	// Calculate position size based on edge and confidence
	positionSize := s.calculatePositionSize(opp.Confidence, opp.Edge)

	utils.Logger.Infof("Position size: $%.2f (%.0f%% of max due to %.1f%% confidence, %.1f%% edge)",
		positionSize, (positionSize/s.config.MaxPositionSize)*100, opp.Confidence*100, opp.Edge*100)

	// PHASE 7: Check gas price profitability
	shouldExecute, reason := s.client.ShouldExecuteTrade(opp.ExpectedProfit, positionSize)
	if !shouldExecute {
		utils.Logger.Warnf("⛽ Skipping trade due to gas cost: %s", reason)
		return fmt.Errorf("gas cost too high: %s", reason)
	}

	// Place market order with retry logic (up to 3 attempts)
	maxRetries := 3
	var orderErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		orderErr = s.client.PlaceMarketOrder(opp.TokenID, opp.CurrentPrice, positionSize)
		if orderErr == nil {
			// Success!
			break
		}

		// Check if error is retryable (price-related) or permanent (balance/auth)
		errorMsg := orderErr.Error()
		isBalanceError := strings.Contains(errorMsg, "not enough balance") || strings.Contains(errorMsg, "allowance")
		isAuthError := strings.Contains(errorMsg, "Unauthorized") || strings.Contains(errorMsg, "Invalid api key")
		isPermanentError := isBalanceError || isAuthError

		if isPermanentError {
			// Don't retry for balance/auth errors - they won't be fixed by retrying
			utils.Logger.Warnf("⚠️  Permanent error (won't retry): %v", orderErr)
			return fmt.Errorf("failed to place order: %w", orderErr)
		}

		// If this was the last attempt, give up
		if attempt == maxRetries {
			utils.Logger.Errorf("Failed to place order after %d attempts: %v", maxRetries, orderErr)
			return fmt.Errorf("failed to place order after %d retries: %w", maxRetries, orderErr)
		}

		// Retry for price-related or transient errors
		utils.Logger.Warnf("Order attempt %d/%d failed (retrying): %v", attempt, maxRetries, orderErr)

		// Fetch fresh market data
		freshMarket, err := s.client.GetMarketByID(opp.MarketID)
		if err != nil {
			utils.Logger.Warnf("Failed to fetch fresh market data: %v", err)
			continue // Try again with same price
		}

		// Get fresh price
		freshPrice := s.getCurrentPrice(*freshMarket, opp.Outcome)
		if freshPrice <= 0 {
			utils.Logger.Warnf("Invalid fresh price, using cached price")
			continue
		}

		// Recalculate profit with fresh price
		newExpectedProfit := (1.0 - freshPrice - PolymarketFee) * 100
		if newExpectedProfit >= s.config.MinProfitThreshold*100 {
			utils.Logger.Infof("🔄 Retrying with updated price: $%.2f (profit: %.2f%%)", freshPrice, newExpectedProfit)
			opp.CurrentPrice = freshPrice
			opp.ExpectedProfit = newExpectedProfit / 100
		} else {
			utils.Logger.Warnf("Fresh price $%.2f no longer profitable (%.2f%% < %.2f%%)",
				freshPrice, newExpectedProfit, s.config.MinProfitThreshold*100)
			return fmt.Errorf("opportunity no longer profitable after price update")
		}
	}

	utils.Logger.Infof("✅ Order placed successfully")

	// Record open position
	position := database.Position{
		MarketID:       opp.MarketID,
		TokenID:        opp.TokenID,
		MarketQuestion: opp.MarketQuestion,
		Outcome:        opp.Outcome,
		EntryPrice:     opp.CurrentPrice,
		PositionSize:   positionSize,
	}

	if err := database.OpenPosition(s.db, position); err != nil {
		utils.Logger.Errorf("Failed to record position: %v", err)
	} else {
		shares := positionSize / opp.CurrentPrice
		utils.Logger.Infof("📊 Position recorded: %.2f shares @ $%.2f", shares, opp.CurrentPrice)
	}

	// Log trade to database
	trade := database.Trade{
		MarketID:       opp.MarketID,
		MarketQuestion: opp.MarketQuestion,
		Outcome:        opp.Outcome,
		EntryPrice:     opp.CurrentPrice,
		ExitPrice:      0, // Will be updated when market resolves
		Profit:         0, // Will be calculated later
		Status:         "PENDING",
	}

	if err := database.LogTrade(s.db, trade); err != nil {
		utils.Logger.Errorf("Failed to log trade: %v", err)
	}

	// Record trade in circuit breaker (with 0 profit for now, will update when resolved)
	if err := s.circuitBreaker.RecordTrade(0); err != nil {
		utils.Logger.Errorf("Circuit breaker error: %v", err)
		return fmt.Errorf("circuit breaker error: %w", err)
	}

	// PHASE 7: Record execution to prevent duplicates
	s.executedTrades.RecordExecution(opp.MarketID, opp.Outcome)

	// Send notification if configured
	if s.config.DiscordWebhookURL != "" {
		message := fmt.Sprintf("🤖 **Trade Executed**\n"+
			"Market: %s\n"+
			"Outcome: %s\n"+
			"Entry Price: $%.2f\n"+
			"Position: $%.2f\n"+
			"Expected Profit: %.2f%%\n"+
			"Confidence: %.1f%%",
			opp.MarketQuestion, opp.Outcome, opp.CurrentPrice,
			positionSize, opp.ExpectedProfit*100, opp.Confidence*100)

		if err := utils.SendDiscordNotification(s.config.DiscordWebhookURL, message); err != nil {
			utils.Logger.Errorf("Failed to send notification: %v", err)
		}
	}

	return nil
}

// calculatePositionSize determines position size based on confidence level and edge
// Uses a Kelly-like approach: higher edge = larger position, higher confidence = larger position
// Formula: baseSize * confidenceMultiplier * edgeMultiplier
func (s *OracleLagStrategy) calculatePositionSize(confidence float64, edge float64) float64 {
	baseSize := s.config.MaxPositionSize

	// Confidence multiplier
	var confidenceMultiplier float64
	switch {
	case confidence >= 0.95:
		// Very high confidence (95%+): 100%
		confidenceMultiplier = 1.0
	case confidence >= 0.85:
		// High confidence (85-95%): 90%
		confidenceMultiplier = 0.9
	case confidence >= 0.75:
		// Medium-high confidence (75-85%): 75%
		confidenceMultiplier = 0.75
	case confidence >= 0.66:
		// Medium confidence (66-75%): 60%
		confidenceMultiplier = 0.6
	default:
		// Below minimum confidence threshold: no trade (shouldn't reach here)
		return 0
	}

	// Edge multiplier - scale position by edge strength
	var edgeMultiplier float64
	switch {
	case edge >= 0.25:
		// Huge edge (25%+): full size
		edgeMultiplier = 1.0
	case edge >= 0.15:
		// Large edge (15-25%): 90%
		edgeMultiplier = 0.9
	case edge >= 0.10:
		// Good edge (10-15%): 75%
		edgeMultiplier = 0.75
	case edge >= s.config.MinProfitThreshold:
		// Minimum edge (threshold-10%): 50%
		edgeMultiplier = 0.5
	default:
		// Below threshold (shouldn't reach here due to earlier filter)
		return 0
	}

	// Combine multipliers
	finalSize := baseSize * confidenceMultiplier * edgeMultiplier

	// Ensure minimum size of at least $1
	if finalSize < 1.0 && finalSize > 0 {
		finalSize = 1.0
	}

	return finalSize
}

// getCurrentPrice gets the current price for an outcome
func (s *OracleLagStrategy) getCurrentPrice(market polymarket.Market, outcome string) float64 {
	if len(market.Prices) == 0 {
		return 0
	}

	// Match outcome to the outcomes list (case-insensitive)
	outcomeLower := strings.ToLower(outcome)
	for i, o := range market.Outcomes {
		if strings.ToLower(o) == outcomeLower && i < len(market.Prices) {
			return market.Prices[i]
		}
	}

	// Fallback: YES/Yes = index 0, NO/No = index 1
	outcomeIndex := 0
	if outcomeLower == "no" {
		outcomeIndex = 1
	}

	if outcomeIndex < len(market.Prices) {
		return market.Prices[outcomeIndex]
	}

	return 0
}

// getDynamicThreshold calculates profit threshold based on current performance
// PHASE 7: Dynamic adjustment based on circuit breaker state and recent activity
func (s *OracleLagStrategy) getDynamicThreshold() float64 {
	baseThreshold := s.config.MinProfitThreshold

	// Get current circuit breaker state
	dailyPnL := s.circuitBreaker.GetDailyPnL()
	tradeCount := s.circuitBreaker.GetTradeCount()

	// Increase selectivity if approaching loss limits (be more conservative)
	if dailyPnL < -400 {
		baseThreshold *= 1.5
		utils.Logger.Debugf("🛡️  Increased profit threshold to %.2f%% (daily P&L: $%.2f)",
			baseThreshold*100, dailyPnL)
	} else if dailyPnL < -200 {
		baseThreshold *= 1.2
	}

	// Increase selectivity if approaching trade limit
	if tradeCount > 40 {
		baseThreshold *= 1.3
		utils.Logger.Debugf("🛡️  Increased profit threshold to %.2f%% (trade count: %d)",
			baseThreshold*100, tradeCount)
	} else if tradeCount > 30 {
		baseThreshold *= 1.1
	}

	// Get recent opportunities from last hour (would need to track this)
	// For now, we'll keep it simple with just circuit breaker adjustments

	return baseThreshold
}

// RedemptionResult holds the summary of a position redemption cycle
type RedemptionResult struct {
	Wins       int
	Losses     int
	TotalProfit float64
	Pending    int
}

// CheckAndClaimPositions checks all open positions and claims resolved ones
func (s *OracleLagStrategy) CheckAndClaimPositions(ctx context.Context) (*RedemptionResult, error) {
	positions, err := database.GetOpenPositions(s.db)
	if err != nil {
		return nil, fmt.Errorf("failed to get open positions: %w", err)
	}

	if len(positions) == 0 {
		return &RedemptionResult{}, nil
	}

	utils.Logger.Infof("Checking %d open positions for resolution...", len(positions))

	result := &RedemptionResult{}

	for _, pos := range positions {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		market, err := s.client.GetMarketByID(pos.MarketID)
		if err != nil {
			utils.Logger.Errorf("Failed to fetch market %s for position #%d: %v", pos.MarketID, pos.ID, err)
			result.Pending++
			continue
		}

		if !market.Closed {
			result.Pending++
			continue
		}

		// Market is closed - determine win or loss
		if market.ResolvedOutcome != nil && strings.EqualFold(*market.ResolvedOutcome, pos.Outcome) {
			// WIN - our outcome was correct
			if err := database.ClaimPosition(s.db, pos.ID, 1.0); err != nil {
				utils.Logger.Errorf("Failed to claim position #%d: %v", pos.ID, err)
				continue
			}

			profit := pos.Shares - pos.PositionSize
			result.Wins++
			result.TotalProfit += profit

			utils.Logger.Infof("🏆 POSITION WON #%d: %s | %s | Profit: $%.2f",
				pos.ID, pos.MarketQuestion, pos.Outcome, profit)

			// Send Discord notification
			if s.config.DiscordWebhookURL != "" {
				msg := fmt.Sprintf("🏆 **Position Won!**\n"+
					"Market: %s\n"+
					"Outcome: %s\n"+
					"Entry: $%.2f | Exit: $1.00\n"+
					"Profit: $%.2f (%.1f%%)",
					pos.MarketQuestion, pos.Outcome,
					pos.EntryPrice, profit, (profit/pos.PositionSize)*100)
				if err := utils.SendDiscordNotification(s.config.DiscordWebhookURL, msg); err != nil {
					utils.Logger.Errorf("Failed to send win notification: %v", err)
				}
			}
		} else {
			// LOSS - our outcome was wrong
			if err := database.MarkPositionLost(s.db, pos.ID); err != nil {
				utils.Logger.Errorf("Failed to mark position #%d as lost: %v", pos.ID, err)
				continue
			}

			result.Losses++
			result.TotalProfit -= pos.PositionSize

			utils.Logger.Infof("❌ POSITION LOST #%d: %s | %s | Loss: -$%.2f",
				pos.ID, pos.MarketQuestion, pos.Outcome, pos.PositionSize)

			// Send Discord notification
			if s.config.DiscordWebhookURL != "" {
				msg := fmt.Sprintf("❌ **Position Lost**\n"+
					"Market: %s\n"+
					"Outcome: %s\n"+
					"Loss: -$%.2f",
					pos.MarketQuestion, pos.Outcome, pos.PositionSize)
				if err := utils.SendDiscordNotification(s.config.DiscordWebhookURL, msg); err != nil {
					utils.Logger.Errorf("Failed to send loss notification: %v", err)
				}
			}
		}
	}

	return result, nil
}

// getTokenIDForOutcome finds the ERC1155 token ID for a specific outcome
func (s *OracleLagStrategy) getTokenIDForOutcome(market polymarket.Market, outcome string) string {
	outcomeLower := strings.ToLower(outcome)

	// Match outcome to token ID by index
	for i, o := range market.Outcomes {
		if strings.ToLower(o) == outcomeLower && i < len(market.TokenIDs) {
			return market.TokenIDs[i]
		}
	}

	// Fallback: Yes=0, No=1
	idx := 0
	if outcomeLower == "no" {
		idx = 1
	}
	if idx < len(market.TokenIDs) {
		return market.TokenIDs[idx]
	}

	return ""
}
