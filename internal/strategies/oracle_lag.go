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
	// Hard limit: never re-enter within 4 hours of an existing open position entry
	ReEntryHardLimit = 4 * time.Hour
	// Re-entry position size multiplier (50% of normal size)
	ReEntryPositionMultiplier = 0.5
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

// ScanResult holds statistics from a single scan cycle
type ScanResult struct {
	MarketsScanned int
	Opportunities  int
	Executed       int
	Skipped        int
	SkipReasons    map[string]int // reason -> count
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
	Reasoning      string // Human-readable explanation of why this trade was selected
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

// ScanOpportunities scans for arbitrage opportunities.
// It returns two channels: one for opportunities and one for the final ScanResult
// (which receives exactly one value after the scan goroutine finishes).
func (s *OracleLagStrategy) ScanOpportunities(ctx context.Context) (<-chan Opportunity, <-chan ScanResult) {
	opportunitiesChan := make(chan Opportunity, 50)
	resultChan := make(chan ScanResult, 1)

	go func() {
		defer close(opportunitiesChan)
		defer close(resultChan)

		result := ScanResult{
			SkipReasons: make(map[string]int),
		}
		// Check circuit breaker first
		canTrade, reason := s.circuitBreaker.CanTrade()
		if !canTrade {
			utils.Logger.Errorf("🚨 Circuit breaker is tripped: %s", reason)
			utils.Logger.Error("Trading is DISABLED. Reset required.")
			resultChan <- result
			return
		}

		// Get markets past resolution
		utils.Logger.Info("Scanning for opportunities...")
		markets, err := s.monitor.GetMarketsPastResolution(ctx)
		if err != nil {
			utils.Logger.Errorf("Failed to get markets: %v", err)
			resultChan <- result
			return
		}

		result.MarketsScanned = len(markets)
		utils.Logger.Infof("Checking %d markets for opportunities...", len(markets))

		// Concurrent market scanning: up to 10 simultaneous IEM requests
		sem := make(chan struct{}, 10)
		var wg sync.WaitGroup
		var resultMu sync.Mutex

		for _, market := range markets {
			select {
			case <-ctx.Done():
				break
			default:
			}

			wg.Add(1)
			go func(m polymarket.Market) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				select {
				case <-ctx.Done():
					return
				default:
				}

				localSkip := func(reason string) {
					resultMu.Lock()
					result.Skipped++
					result.SkipReasons[reason]++
					resultMu.Unlock()
				}

				// Apply market quality filter
				if shouldSkip, filterReason := s.marketFilter.ShouldSkip(m); shouldSkip {
					utils.Logger.Debugf("Skipping market %s: %s", m.Question, filterReason)
					localSkip("market_filter")
					return
				}

				// Get appropriate resolver
				category := s.monitor.CategorizeMarket(m)
				resolver := s.factory.GetResolver(category)
				if resolver == nil {
					resolver = s.factory.GetResolverByQuestion(m.Question)
					if resolver == nil {
						utils.Logger.Debugf("No resolver found for market: %s", m.Question)
						localSkip("no_resolver")
						return
					}
				}

				// Check if market can be resolved
				outcome, confidence, err := resolver.CheckResolution(m)
				if err != nil {
					utils.Logger.Debugf("Resolution check failed for %s: %v", m.Question, err)
					localSkip("resolution_error")
					return
				}

				if outcome == nil {
					utils.Logger.Debugf("Market not yet resolvable: %s", m.Question)
					localSkip("not_resolvable")
					return
				}

				if confidence < 0.66 {
					utils.Logger.Debugf("SKIP [low confidence] %s: confidence=%.1f%%", m.Question, confidence*100)
					localSkip("low_confidence")
					return
				}

				yesPrice := s.getCurrentPrice(m, "YES")
				noPrice := s.getCurrentPrice(m, "NO")

				if yesPrice <= 0 || noPrice <= 0 {
					utils.Logger.Debugf("SKIP [invalid prices] %s: YES=$%.2f NO=$%.2f", m.Question, yesPrice, noPrice)
					localSkip("invalid_prices")
					return
				}

				if yesPrice < 0.05 || noPrice < 0.05 {
					utils.Logger.Debugf("SKIP [dead market] %s: YES=$%.2f NO=$%.2f", m.Question, yesPrice, noPrice)
					localSkip("dead_market")
					return
				}

				bestOutcome := *outcome
				var bestPrice, bestEdge float64
				if bestOutcome == "Yes" {
					bestPrice = yesPrice
					bestEdge = confidence - yesPrice
				} else {
					bestPrice = noPrice
					bestEdge = confidence - noPrice
				}

				dynamicThreshold := s.getDynamicThreshold()
				if bestEdge < dynamicThreshold {
					utils.Logger.Debugf("SKIP [edge too low] %s: edge=%.2f%% threshold=%.2f%%",
						m.Question, bestEdge*100, dynamicThreshold*100)
					localSkip("low_edge")
					return
				}

				expectedProfit := bestEdge - PolymarketFee

				tokenID := s.getTokenIDForOutcome(m, bestOutcome)
				if tokenID == "" {
					utils.Logger.Debugf("SKIP [no token ID] %s", m.Question)
					localSkip("no_token_id")
					return
				}

				confidenceMargin := confidence - 0.5
				reasoning := fmt.Sprintf(
					"Resolver outcome: %s\n  Confidence: %.0f%% (margin: +%.1f%% above 50%%)\n  Edge: %.1f%% (price: $%.2f)\n  Expected profit: %.1f%%\n  Data source: IEM ASOS",
					bestOutcome, confidence*100, confidenceMargin*100, bestEdge*100, bestPrice, expectedProfit*100,
				)

				opp := Opportunity{
					MarketID:       m.ID,
					MarketQuestion: m.Question,
					Outcome:        bestOutcome,
					TokenID:        tokenID,
					CurrentPrice:   bestPrice,
					ExpectedProfit: expectedProfit,
					Edge:           bestEdge,
					Confidence:     confidence,
					Lag:            s.monitor.CalculateLag(m),
					Timestamp:      time.Now(),
					Reasoning:      reasoning,
				}

				utils.Logger.Infof("🎯 OPPORTUNITY FOUND: %s | %s | Price: $%.2f | Edge: %.2f%% | Profit: %.2f%%",
					m.Question, bestOutcome, bestPrice, bestEdge*100, expectedProfit*100)

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

				resultMu.Lock()
				result.Opportunities++
				resultMu.Unlock()

				select {
				case <-ctx.Done():
					return
				case opportunitiesChan <- opp:
				}
			}(market)
		}

		wg.Wait()

		utils.Logger.Info("Opportunity scan complete")
		resultChan <- result
	}()

	return opportunitiesChan, resultChan
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

	// Fetch live balance for dynamic position sizing
	balance, balErr := s.client.GetBalance()
	if balErr != nil {
		utils.Logger.Warnf("Failed to fetch live balance, falling back to MaxPositionSize: %v", balErr)
		balance = 0
	} else {
		utils.Logger.Infof("Live balance: $%.2f | PositionSizePct: %.1f%%", balance, s.config.PositionSizePct*100)
	}

	// Calculate position size based on edge, confidence, live balance, and market question
	positionSize := s.calculatePositionSize(opp.Confidence, opp.Edge, balance, opp.MarketQuestion)

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
		message := utils.TradeExecutedMessage(opp.MarketQuestion, opp.Outcome, opp.CurrentPrice, positionSize, opp.ExpectedProfit, opp.Confidence, opp.Reasoning)
		if err := utils.SendDiscordNotification(s.config.DiscordWebhookURL, message); err != nil {
			utils.Logger.Errorf("Failed to send notification: %v", err)
		}
	}

	return nil
}

// calculatePositionSize determines position size based on available balance, confidence, and edge.
// baseSize is derived from balance * PositionSizePct, capped at MaxPositionSize.
// If balance is <= 0 (e.g. fetch failed), falls back to MaxPositionSize.
// Rain markets are hard-capped at RainMaxPosition (default $1.00) for testing.
// Formula: baseSize * confidenceMultiplier * edgeMultiplier
func (s *OracleLagStrategy) calculatePositionSize(confidence float64, edge float64, balance float64, marketQuestion string) float64 {
	var baseSize float64
	if balance > 0 {
		baseSize = balance * s.config.PositionSizePct
		// Never exceed the configured hard cap
		if baseSize > s.config.MaxPositionSize {
			baseSize = s.config.MaxPositionSize
		}
	} else {
		// Fallback when live balance is unavailable
		baseSize = s.config.MaxPositionSize
	}

	// Confidence multiplier
	var confidenceMultiplier float64
	switch {
	case confidence >= 0.95:
		// Very high confidence (95%+): 100%
		confidenceMultiplier = 1.0
	case confidence >= 0.85:
		// High confidence (85-95%): 95%
		confidenceMultiplier = 0.95
	case confidence >= 0.75:
		// Medium-high confidence (75-85%): 85%
		confidenceMultiplier = 0.85
	case confidence >= 0.66:
		// Medium confidence (66-75%): 75%
		confidenceMultiplier = 0.75
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
		// Large edge (15-25%): 95%
		edgeMultiplier = 0.95
	case edge >= 0.10:
		// Good edge (10-15%): 85%
		edgeMultiplier = 0.85
	case edge >= s.config.MinProfitThreshold:
		// Minimum edge (threshold-10%): 65%
		edgeMultiplier = 0.65
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

	// Rain markets are capped at RainMaxPosition for testing
	if strings.Contains(strings.ToLower(marketQuestion), "rain") {
		rainCap := s.config.RainMaxPosition
		if rainCap <= 0 {
			rainCap = 1.0
		}
		if finalSize > rainCap {
			utils.Logger.Infof("🌧️  Rain market position capped at $%.2f (was $%.2f): %s",
				rainCap, finalSize, marketQuestion)
			finalSize = rainCap
		}
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

		// Determine win or loss from outcomePrices — winner has price 1.0, loser 0.0.
		// The Gamma API does not populate resolved_outcome; use prices instead.
		won := s.outcomeWon(market.Outcomes, market.Prices, pos.Outcome)
		utils.Logger.Infof("Market closed: %s | Outcomes: %v | Prices: %v | We held: %s | Won: %v",
			pos.MarketQuestion, market.Outcomes, market.Prices, pos.Outcome, won)

		if won {
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
				profitPct := (profit / pos.PositionSize) * 100
				holdDuration := time.Since(pos.EntryTime).Round(time.Minute)
				msg := fmt.Sprintf(
					"🏆 **Position Won!**\n"+
						"📋 **Market:** %s\n"+
						"✅ **Outcome:** %s\n"+
						"💵 **Staked:** $%.2f @ $%.2f entry\n"+
						"💰 **Profit:** +$%.2f (+%.1f%%)\n"+
						"⏱️ **Held for:** %s",
					pos.MarketQuestion,
					pos.Outcome,
					pos.PositionSize, pos.EntryPrice,
					profit, profitPct,
					holdDuration.String(),
				)
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
				holdDuration := time.Since(pos.EntryTime).Round(time.Minute)
				msg := fmt.Sprintf(
					"❌ **Position Lost**\n"+
						"📋 **Market:** %s\n"+
						"🚫 **Outcome:** %s\n"+
						"💵 **Staked:** $%.2f @ $%.2f entry\n"+
						"📉 **Loss:** -$%.2f\n"+
						"⏱️ **Held for:** %s",
					pos.MarketQuestion,
					pos.Outcome,
					pos.PositionSize, pos.EntryPrice,
					pos.PositionSize,
					holdDuration.String(),
				)
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

// outcomeWon checks if the held outcome won by finding its price in outcomePrices.
// Polymarket sets the winning outcome price to 1.0 and losing to 0.0 after resolution.
func (s *OracleLagStrategy) outcomeWon(outcomes []string, prices []float64, heldOutcome string) bool {
	for i, o := range outcomes {
		if strings.EqualFold(o, heldOutcome) && i < len(prices) {
			return prices[i] >= 0.99
		}
	}
	return false
}
