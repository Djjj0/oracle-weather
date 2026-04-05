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
	// SlippageBuffer accounts for estimated price impact of a market buy order
	SlippageBuffer = 0.05
)

// tradeRecord stores metadata about a trade for re-entry tracking
type tradeRecord struct {
	firstTradeTime time.Time
	reEntryDone    bool
}

// TradeStatus indicates whether a trade should be blocked, allowed as re-entry, or new
type TradeStatus int

const (
	TradeStatusNew     TradeStatus = iota // No prior trade, full size
	TradeStatusReEntry                    // 1-4h after first trade, 50% size
	TradeStatusBlocked                    // < 1h or re-entry already done
)

// ExecutedTrades tracks recently executed trades to prevent duplicates
type ExecutedTrades struct {
	trades map[string]tradeRecord // marketID:outcome -> record
	mu     sync.RWMutex
}

// NewExecutedTrades creates a new executed trades tracker
func NewExecutedTrades() *ExecutedTrades {
	return &ExecutedTrades{
		trades: make(map[string]tradeRecord),
	}
}

// CheckTradeStatus returns whether a trade is new, a valid re-entry, or blocked
func (et *ExecutedTrades) CheckTradeStatus(marketID, outcome string) TradeStatus {
	et.mu.RLock()
	defer et.mu.RUnlock()

	key := marketID + ":" + outcome
	rec, exists := et.trades[key]
	if !exists {
		return TradeStatusNew
	}

	elapsed := time.Since(rec.firstTradeTime)
	if elapsed < MinReExecutionDelay {
		return TradeStatusBlocked // Too soon
	}
	if elapsed > ReEntryHardLimit || rec.reEntryDone {
		return TradeStatusNew // Past hard limit or re-entry already done — treat as fresh
	}
	return TradeStatusReEntry
}

// AlreadyExecuted checks if a trade was recently executed (kept for compatibility)
func (et *ExecutedTrades) AlreadyExecuted(marketID, outcome string) bool {
	return et.CheckTradeStatus(marketID, outcome) == TradeStatusBlocked
}

// RecordExecution records a trade execution
func (et *ExecutedTrades) RecordExecution(marketID, outcome string, isReEntry bool) {
	et.mu.Lock()
	defer et.mu.Unlock()

	key := marketID + ":" + outcome
	rec, exists := et.trades[key]
	if !exists || time.Since(rec.firstTradeTime) > ReEntryHardLimit {
		et.trades[key] = tradeRecord{firstTradeTime: time.Now()}
	} else if isReEntry {
		rec.reEntryDone = true
		et.trades[key] = rec
	}

	// Clean up old entries (>24 hours) - prevent memory leak
	for k, r := range et.trades {
		if time.Since(r.firstTradeTime) > ExecutedTradesCleanup {
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
	SkipReasons    map[string]int            // reason -> count
	SkipByCity     map[string]map[string]int // city -> reason -> count
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
	DBOpportunityID uint64 // BoltDB ID of the logged Opportunity record (for flip-to-executed)
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

// cityQuestionRe extracts city name from weather market questions.
var cityQuestionRe = regexp.MustCompile(`(?i)in ([A-Za-z\s]+?) (?:be|on)`)

// extractCityFromQuestion returns the lowercase city name from a market question,
// or "" if the pattern doesn't match.
func extractCityFromQuestion(question string) string {
	if m := cityQuestionRe.FindStringSubmatch(question); len(m) > 1 {
		return strings.ToLower(strings.TrimSpace(m[1]))
	}
	return ""
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
			SkipByCity:  make(map[string]map[string]int),
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

		// Concurrent market scanning: up to 3 simultaneous IEM requests (avoid TLS throttling)
		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		var resultMu sync.Mutex

		for i, market := range markets {
			select {
			case <-ctx.Done():
				break
			default:
			}

			// Stagger resolver dispatches to avoid IEM rate limits
			if i > 0 {
				select {
				case <-ctx.Done():
					break
				case <-time.After(500 * time.Millisecond):
				}
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
					if city := extractCityFromQuestion(m.Question); city != "" {
						if result.SkipByCity[city] == nil {
							result.SkipByCity[city] = make(map[string]int)
						}
						result.SkipByCity[city][reason]++
					}
					resultMu.Unlock()
				}

				// Apply market quality filter
				if shouldSkip, filterReason := s.marketFilter.ShouldSkip(m); shouldSkip {
					utils.Logger.Infof("SKIP [market_filter] %q: %s", m.Question, filterReason)
					localSkip("market_filter: " + filterReason)
					return
				}

				// Pre-flight price check: skip dead/invalid markets before paying the cost
				// of an IEM API call. ~70% of eligible markets are already resolved
				// (one side near zero). Checking prices first avoids ~62k IEM calls/3-days.
				yesPrice := s.getCurrentPrice(m, "YES")
				noPrice := s.getCurrentPrice(m, "NO")

				if yesPrice <= 0 || noPrice <= 0 {
					utils.Logger.Infof("SKIP [invalid_prices] %q: YES=$%.2f NO=$%.2f", m.Question, yesPrice, noPrice)
					localSkip("invalid_prices")
					return
				}

				if yesPrice < 0.05 || noPrice < 0.05 {
					utils.Logger.Debugf("SKIP [dead_market] %q: YES=$%.2f NO=$%.2f (one side near zero)", m.Question, yesPrice, noPrice)
					localSkip("dead_market")
					return
				}

				// Skip markets >2h past resolution still priced near 50/50 — broken/stuck market.
				staleCutoff := m.ResolutionTimestamp.Add(2 * time.Hour)
				if time.Now().After(staleCutoff) {
					const staleLow = 0.40
					const staleHigh = 0.60
					if yesPrice >= staleLow && yesPrice <= staleHigh &&
						noPrice >= staleLow && noPrice <= staleHigh {
						utils.Logger.Infof("SKIP [stale_market] %q: resolution+2h passed, prices still near 50/50 (YES=$%.2f NO=$%.2f)", m.Question, yesPrice, noPrice)
						localSkip("stale_market")
						return
					}
				}

				// Get appropriate resolver
				category := s.monitor.CategorizeMarket(m)
				resolver := s.factory.GetResolver(category)
				if resolver == nil {
					resolver = s.factory.GetResolverByQuestion(m.Question)
					if resolver == nil {
						utils.Logger.Infof("SKIP [no_resolver] %q: no resolver matched category=%s", m.Question, category)
						localSkip("no_resolver")
						return
					}
				}
				resolverName := resolver.Name()

				// Check if market can be resolved
				outcome, confidence, err := resolver.CheckResolution(m)
				if err != nil {
					if strings.Contains(err.Error(), "could not parse") {
						utils.Logger.Debugf("SKIP [resolution_error] %q via %s: %v", m.Question, resolverName, err)
					} else {
						utils.Logger.Infof("SKIP [resolution_error] %q via %s: %v", m.Question, resolverName, err)
					}
					localSkip("resolution_error")
					return
				}

				if outcome == nil {
					utils.Logger.Debugf("SKIP [not_resolvable] %q via %s: not yet actionable (waiting for peak hour or insufficient data)", m.Question, resolverName)
					localSkip("not_resolvable")
					return
				}

				if confidence < 0.66 {
					utils.Logger.Infof("SKIP [low_confidence] %q via %s: confidence=%.1f%% (need >=66%%)", m.Question, resolverName, confidence*100)
					localSkip("low_confidence")
					return
				}

				bestOutcome := *outcome
				var bestPrice, bestEdge float64
				if bestOutcome == "Yes" {
					bestPrice = yesPrice
					bestEdge = confidence - yesPrice - SlippageBuffer
				} else {
					bestPrice = noPrice
					bestEdge = confidence - noPrice - SlippageBuffer
				}

				dynamicThreshold := s.getDynamicThreshold()
				if bestEdge < dynamicThreshold {
					utils.Logger.Infof("SKIP [low_edge] %q via %s: edge=%.2f%% threshold=%.2f%%",
						m.Question, resolverName, bestEdge*100, dynamicThreshold*100)
					localSkip("low_edge")
					return
				}

				expectedProfit := bestEdge - PolymarketFee

				tokenID := s.getTokenIDForOutcome(m, bestOutcome)
				if tokenID == "" {
					utils.Logger.Infof("SKIP [no_token_id] %q: no ERC1155 token ID for outcome %s", m.Question, bestOutcome)
					localSkip("no_token_id")
					return
				}

				confidenceMargin := confidence - 0.5
				reasoning := fmt.Sprintf(
					"Resolver: %s\n  Outcome: %s (confidence: %.0f%%, margin: +%.1f%% above 50%%)\n  Market price: $%.2f\n  Edge: %.1f%%\n  Expected profit after fees: %.1f%%\n  Passed filters: market_quality, confidence>=66%%, edge>=%.1f%%",
					resolverName, bestOutcome, confidence*100, confidenceMargin*100, bestPrice, bestEdge*100, expectedProfit*100, dynamicThreshold*100,
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
				if oppID, err := database.LogOpportunity(s.db, dbOpp); err != nil {
					utils.Logger.Errorf("Failed to log opportunity: %v", err)
				} else {
					opp.DBOpportunityID = oppID
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

		// Log scan summary with per-filter skip breakdown
		utils.Logger.Infof("Scan complete: scanned=%d opportunities=%d skipped=%d",
			result.MarketsScanned, result.Opportunities, result.Skipped)
		if len(result.SkipReasons) > 0 {
			utils.Logger.Info("Skip reason breakdown:")
			for reason, count := range result.SkipReasons {
				utils.Logger.Infof("  %-40s %d", reason, count)
			}
		}
		if result.SkipReasons["dead_market"] > 0 {
			utils.Logger.Debugf("Dead market breakdown by city:")
			for city, reasons := range result.SkipByCity {
				if count := reasons["dead_market"]; count > 0 {
					utils.Logger.Debugf("  %-20s dead=%d not_resolvable=%d low_edge=%d", city, count, reasons["not_resolvable"], reasons["low_edge"])
				}
			}
		}

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

	// Check DB for existing open position on this market — survives restarts.
	// Skip if we already hold a position at a similar or worse price.
	// Allow re-entry only if current price is >10% better than our entry
	// (e.g. we bought NO at 80¢, now it's at 65¢ — materially better entry).
	const minPriceImprovementForReEntry = 0.10
	existingPositions, dbErr := database.GetPositionsByMarket(s.db, opp.MarketID)
	if dbErr == nil {
		for _, p := range existingPositions {
			if p.Status != "OPEN" {
				continue
			}
			// Never place an order for the opposite side — that locks in a loss.
			if !strings.EqualFold(p.Outcome, opp.Outcome) {
				utils.Logger.Warnf("⛔ Skipping %s %s — already hold opposite position (%s) on this market",
					opp.MarketQuestion, opp.Outcome, p.Outcome)
				return nil
			}
			// Same outcome: skip unless price has improved materially.
			priceImprovement := p.EntryPrice - opp.CurrentPrice
			if priceImprovement < minPriceImprovementForReEntry {
				utils.Logger.Infof("⏭️  Skipping - already have open position for %s %s at $%.2f, current $%.2f (improvement %.2f < %.0f%%)",
					opp.MarketQuestion, opp.Outcome, p.EntryPrice, opp.CurrentPrice,
					priceImprovement, minPriceImprovementForReEntry*100)
				return nil
			}
			utils.Logger.Infof("📈 Price improved %.0f%% since entry ($%.2f → $%.2f), adding to position",
				priceImprovement*100, p.EntryPrice, opp.CurrentPrice)
		}
	}

	// In-memory check for duplicate / re-entry within this session
	tradeStatus := s.executedTrades.CheckTradeStatus(opp.MarketID, opp.Outcome)
	isReEntry := false
	switch tradeStatus {
	case TradeStatusBlocked:
		utils.Logger.Infof("⏭️  Skipping - already executed this opportunity within last hour")
		return nil
	case TradeStatusReEntry:
		isReEntry = true
		utils.Logger.Infof("🔄 Re-entry detected for %s — will use 50%% position size", opp.MarketQuestion)
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

	// Subtract current open exposure so we don't over-commit capital.
	if balance > 0 {
		if exposure, expErr := database.GetTotalExposure(s.db); expErr == nil && exposure > 0 {
			utils.Logger.Infof("Open position exposure: $%.2f — adjusting available balance from $%.2f to $%.2f",
				exposure, balance, balance-exposure)
			balance -= exposure
			if balance < 0 {
				balance = 0
			}
		}
	}

	// Calculate position size using half-Kelly criterion
	positionSize := s.calculatePositionSize(opp.Confidence, opp.CurrentPrice, balance, opp.MarketQuestion)
	if isReEntry {
		positionSize *= ReEntryPositionMultiplier
		if positionSize < 5.0 {
			// Polymarket minimum order is $5. Rather than reject the re-entry entirely,
			// floor it at $5 so the order actually fills. For base trades < $10 this means
			// the re-entry will exceed 50% of the original size — acceptable given the
			// improved entry price that triggered the re-entry.
			positionSize = 5.0
		}
		utils.Logger.Infof("🔄 Re-entry position size: $%.2f (50%% of normal, min $5)", positionSize)
	}

	// Enforce 5-share minimum: compute shares from current price and bump positionSize if needed.
	// NOTE: sharesAmount is computed inside the retry loop (Fix #4) so it always reflects
	// the latest opp.CurrentPrice after a price update on retry.
	minSharesCheck := positionSize / opp.CurrentPrice
	if minSharesCheck < 5.0 {
		positionSize = math.Max(5.0*opp.CurrentPrice, 5.0)
		utils.Logger.Infof("Position bumped to 5-share minimum (cost: $%.2f at price $%.2f)", positionSize, opp.CurrentPrice)
	}

	utils.Logger.Infof("Position size: $%.2f (%.0f%% of max due to %.1f%% confidence, %.1f%% edge)",
		positionSize, (positionSize/s.config.MaxPositionSize)*100, opp.Confidence*100, opp.Edge*100)

	// PHASE 7: Check gas price profitability
	shouldExecute, reason := s.client.ShouldExecuteTrade(opp.ExpectedProfit, positionSize)
	if !shouldExecute {
		utils.Logger.Warnf("⛽ Skipping trade due to gas cost: %s", reason)
		return fmt.Errorf("gas cost too high: %s", reason)
	}

	// Place market order with retry logic (up to 3 attempts).
	// sharesAmount is recomputed on every attempt so a price update on retry yields
	// the correct share count for the new price (Fix #4 — stale shares bug).
	// PlaceMarketOrder / CreateOrder expects size in USDC dollars, not shares (Fix #3).
	maxRetries := 3
	var orderErr error
	var placedOrderID, placedOrderStatus string
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Recompute shares from the current (possibly updated) price each attempt.
		sharesAmount := positionSize / opp.CurrentPrice
		utils.Logger.Debugf("Order attempt %d/%d: price=$%.4f size=$%.2f shares=%.2f",
			attempt, maxRetries, opp.CurrentPrice, positionSize, sharesAmount)

		placedOrderID, placedOrderStatus, orderErr = s.client.PlaceMarketOrder(opp.TokenID, opp.CurrentPrice, positionSize)
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
			// positionSize stays the same in USDC; sharesAmount will be recomputed next iteration
		} else {
			utils.Logger.Warnf("Fresh price $%.2f no longer profitable (%.2f%% < %.2f%%)",
				freshPrice, newExpectedProfit, s.config.MinProfitThreshold*100)
			return fmt.Errorf("opportunity no longer profitable after price update")
		}
	}

	isLiveOrder := strings.EqualFold(placedOrderStatus, "live") || strings.EqualFold(placedOrderStatus, "open")
	if isLiveOrder {
		utils.Logger.Infof("⏳ Order submitted but resting in book (status=%s, orderID=%s) — will cancel if unfilled after 10 minutes",
			placedOrderStatus, placedOrderID)
	} else {
		utils.Logger.Infof("✅ Order placed and matched (status=%s, orderID=%s)", placedOrderStatus, placedOrderID)
	}

	// Flip the Executed flag on the logged opportunity record now that the order succeeded.
	if opp.DBOpportunityID > 0 {
		if err := database.MarkOpportunityExecuted(s.db, opp.DBOpportunityID); err != nil {
			utils.Logger.Warnf("Failed to mark opportunity #%d as executed: %v", opp.DBOpportunityID, err)
		}
	}

	// Record open position
	position := database.Position{
		MarketID:       opp.MarketID,
		TokenID:        opp.TokenID,
		MarketQuestion: opp.MarketQuestion,
		Outcome:        opp.Outcome,
		EntryPrice:     opp.CurrentPrice,
		PositionSize:   positionSize,
		OrderID:        placedOrderID,
	}

	var positionID uint64
	if err := database.OpenPosition(s.db, position); err != nil {
		utils.Logger.Errorf("Failed to record position: %v", err)
	} else {
		shares := positionSize / opp.CurrentPrice
		utils.Logger.Infof("📊 Position recorded: %.2f shares @ $%.2f", shares, opp.CurrentPrice)
		// Retrieve the ID so we can cancel it later if needed.
		if positions, err := database.GetPositionsByMarket(s.db, opp.MarketID); err == nil {
			for _, p := range positions {
				if p.Status == "OPEN" && p.OrderID == placedOrderID {
					positionID = p.ID
					break
				}
			}
		}
	}

	// If the order is resting unfilled, launch a watchdog goroutine that cancels
	// it after 10 minutes and removes the in-memory execution record so the
	// opportunity can be re-evaluated on the next scan cycle.
	if isLiveOrder && placedOrderID != "" && positionID != 0 {
		go s.watchAndCancelOrder(placedOrderID, positionID, opp.MarketID, opp.Outcome, opp.MarketQuestion)
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

	// Record execution to prevent duplicates / track re-entry
	s.executedTrades.RecordExecution(opp.MarketID, opp.Outcome, isReEntry)

	// Send notification if configured
	if s.config.DiscordWebhookURL != "" {
		message := utils.TradeExecutedMessage(opp.MarketQuestion, opp.Outcome, opp.CurrentPrice, positionSize, opp.ExpectedProfit, opp.Confidence, opp.Reasoning)
		if err := utils.SendDiscordNotification(s.config.DiscordWebhookURL, message); err != nil {
			utils.Logger.Errorf("Failed to send notification: %v", err)
		}
	}

	return nil
}

// watchAndCancelOrder waits 10 minutes and cancels the order if it is still
// resting unfilled in the Polymarket order book. Once cancelled the position
// is marked CANCELLED in the database and the in-memory execution record is
// removed so the scanner can re-evaluate the opportunity on the next cycle.
func (s *OracleLagStrategy) watchAndCancelOrder(orderID string, positionID uint64, marketID, outcome, question string) {
	const openOrderTimeout = 10 * time.Minute
	utils.Logger.Infof("⏱️  Open-order watchdog started for %s %s (orderID=%s, timeout=%s)",
		question, outcome, orderID, openOrderTimeout)

	timer := time.NewTimer(openOrderTimeout)
	defer timer.Stop()
	<-timer.C

	// Check whether the position is still OPEN (it may have been filled and
	// updated externally, e.g. by a future fill-reconciliation loop).
	positions, err := database.GetPositionsByMarket(s.db, marketID)
	if err == nil {
		for _, p := range positions {
			if p.ID == positionID && p.Status != "OPEN" {
				utils.Logger.Infof("⏱️  Watchdog: position %d is no longer OPEN (status=%s) — skipping cancel",
					positionID, p.Status)
				return
			}
		}
	}

	utils.Logger.Warnf("⏱️  Watchdog: order %s still unfilled after %s — cancelling", orderID, openOrderTimeout)

	if err := s.client.CancelOrder(orderID); err != nil {
		utils.Logger.Errorf("⏱️  Watchdog: failed to cancel order %s: %v", orderID, err)
		// Don't clean up in-memory state — leave the block in place so we don't
		// accidentally re-enter while the order status is unknown.
		return
	}

	if err := database.CancelPosition(s.db, positionID); err != nil {
		utils.Logger.Errorf("⏱️  Watchdog: failed to mark position %d as CANCELLED: %v", positionID, err)
	}

	// Remove the in-memory execution record so the next scan can re-evaluate.
	s.executedTrades.mu.Lock()
	delete(s.executedTrades.trades, marketID+":"+outcome)
	s.executedTrades.mu.Unlock()

	utils.Logger.Infof("⏱️  Watchdog: order %s cancelled and position %d marked CANCELLED — market will be re-evaluated on next scan",
		orderID, positionID)
}

// calculatePositionSize uses the half-Kelly criterion to size each position.
//
// Kelly formula for a binary market:
//
//	f* = (confidence - price) / (1 - price)
//
// where price is the cost per share and 1.0 is the payout on a winning share.
// We use half-Kelly (f = f*/2) to reduce variance while preserving the
// proportional sizing benefit: higher-confidence, lower-priced bets get larger
// allocations automatically, without needing hand-tuned bracket multipliers.
//
// The result is capped at MaxPositionSize and floored at $5 (Polymarket minimum).
// Rain markets retain their hard cap for testing.
//
// If balance is unavailable (<= 0), falls back to MaxPositionSize.
func (s *OracleLagStrategy) calculatePositionSize(confidence, currentPrice float64, balance float64, marketQuestion string) float64 {
	// Guard: Kelly is undefined or negative when confidence <= price.
	// This shouldn't reach here (edge filter blocks it earlier), but be safe.
	if currentPrice <= 0 || currentPrice >= 1.0 || confidence <= currentPrice {
		return 0
	}

	// Half-Kelly fraction of bankroll.
	// f* = (confidence - price) / (1 - price)
	// f_half = f* / 2
	kellyFull := (confidence - currentPrice) / (1.0 - currentPrice)
	kellyHalf := kellyFull / 2.0

	// Determine available capital.
	var availableBalance float64
	if balance > 0 {
		availableBalance = balance
	} else {
		// Fallback: no live balance — use MaxPositionSize directly.
		utils.Logger.Debugf("Kelly sizing: no live balance, using MaxPositionSize fallback")
		return s.config.MaxPositionSize
	}

	finalSize := availableBalance * kellyHalf

	// Hard cap: never exceed MaxPositionSize per trade regardless of Kelly output.
	if finalSize > s.config.MaxPositionSize {
		finalSize = s.config.MaxPositionSize
	}

	utils.Logger.Infof("📐 Kelly sizing: conf=%.0f%% price=$%.2f → f*=%.1f%% half-f=%.1f%% → $%.2f (balance=$%.2f cap=$%.2f)",
		confidence*100, currentPrice, kellyFull*100, kellyHalf*100, finalSize, availableBalance, s.config.MaxPositionSize)

	// Ensure minimum of $5 — Polymarket rejects orders below this threshold.
	if finalSize < 5.0 && finalSize > 0 {
		finalSize = 5.0
	}

	// Rain markets are capped at RainMaxPosition for testing.
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

// recordLearning writes the resolved market outcome to the learning DB so that
// OptimalEntryHour is refined over time from actual trade results.
func (s *OracleLagStrategy) recordLearning(pos database.Position, success bool) {
	// Extract city from market question (e.g. "Will the highest temperature in London be...")
	cityRe := regexp.MustCompile(`(?i)in ([A-Za-z\s]+?) (?:be|on)`)
	city := ""
	if m := cityRe.FindStringSubmatch(pos.MarketQuestion); len(m) > 1 {
		city = strings.TrimSpace(m[1])
	}
	if city == "" {
		return
	}

	ldb := s.factory.LearningDBForCity(city)
	if ldb == nil {
		return
	}

	// Look up timezone for this city
	var tz string
	if stats, err := ldb.GetCityStats(strings.ToLower(city)); err == nil {
		tz = stats.Timezone
	}
	if tz == "" {
		tz = "UTC"
	}

	// Use position entry time as the "optimal entry" observation point —
	// over many trades this converges to the real optimal hour.
	err := ldb.RecordMarketOutcome(
		pos.MarketID,
		city,
		tz,
		pos.EntryTime, // market date (close enough for daily granularity)
		0,            // high temp unknown here; leave zero
		pos.EntryTime,
		pos.EntryTime,
		success,
	)
	if err != nil {
		utils.Logger.Debugf("Learning DB update for %s: %v", city, err)
	} else {
		utils.Logger.Debugf("Learning DB updated for %s (success=%v)", city, success)
	}
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
			// 403/404 means market is no longer accessible — likely resolved and archived.
			// Stop retrying; record as a loss to clear it from open positions.
			errStr := err.Error()
			if strings.Contains(errStr, "403") || strings.Contains(errStr, "404") || strings.Contains(errStr, "Forbidden") {
				utils.Logger.Warnf("⚠️  Position #%d (%s): market no longer accessible (%v) — marking as lost",
					pos.ID, pos.MarketQuestion, err)
				database.MarkPositionLost(s.db, pos.ID)
				result.Losses++
				continue
			}
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
			// WIN — redeem on-chain first so USDC lands in the wallet
			if pos.TokenID != "" {
				redeemResp, redeemErr := polymarket.RedeemPositionViaPython(ctx, pos.TokenID, pos.Outcome)
				if redeemErr != nil {
					utils.Logger.Errorf("On-chain redemption failed for position #%d (%s): %v — will retry next cycle",
						pos.ID, pos.MarketQuestion, redeemErr)
					result.Pending++
					continue
				}
				if !redeemResp.Success {
					// "No balance for token" means tokens are already gone — either already
					// redeemed externally or this is a losing position miscategorised as a win.
					// Stop retrying and close it out rather than spamming every cycle.
					alreadyGone := strings.Contains(redeemResp.Error, "No balance for token") ||
						strings.Contains(redeemResp.Error, "no balance") ||
						strings.Contains(redeemResp.Error, "403") ||
						strings.Contains(redeemResp.Error, "Forbidden")
					if alreadyGone {
						utils.Logger.Warnf("⚠️  Position #%d (%s): tokens already redeemed or no on-chain balance — closing position",
							pos.ID, pos.MarketQuestion)
						database.ClaimPosition(s.db, pos.ID, 1.0)
						result.Wins++ // tokens were already claimed; record as win
						continue
					}
					utils.Logger.Errorf("On-chain redemption rejected for position #%d (%s): %s — will retry next cycle",
						pos.ID, pos.MarketQuestion, redeemResp.Error)
					result.Pending++
					continue
				}
			} else {
				utils.Logger.Warnf("No TokenID for position #%d (%s) — skipping on-chain redemption, recording win only",
					pos.ID, pos.MarketQuestion)
			}

			if err := database.ClaimPosition(s.db, pos.ID, 1.0); err != nil {
				utils.Logger.Errorf("Failed to claim position #%d: %v", pos.ID, err)
				continue
			}

			profit := pos.Shares - pos.PositionSize
			result.Wins++
			result.TotalProfit += profit

			utils.Logger.Infof("🏆 POSITION WON #%d: %s | %s | Profit: $%.2f",
				pos.ID, pos.MarketQuestion, pos.Outcome, profit)

			go s.recordLearning(pos, true)

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

			// Notify circuit breaker of the realized loss so daily loss limits are enforced.
			// Use RecordLoss (not RecordTrade) to avoid double-counting this trade toward
			// the daily trade limit — it was already counted when the order was placed.
			if err := s.circuitBreaker.RecordLoss(-pos.PositionSize); err != nil {
				utils.Logger.Errorf("Circuit breaker tripped on loss: %v", err)
			}

			go s.recordLearning(pos, false)

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

// ValidateAndExitBadPositions re-runs the IEM resolver against every open position.
// Any position where the resolver disagrees (or says it's too early to know) is sold
// immediately at the current market price. This catches trades placed by old/buggy code
// before peak-hour gating was enforced.
func (s *OracleLagStrategy) ValidateAndExitBadPositions(ctx context.Context) error {
	utils.Logger.Info("🔎 Validating open positions against IEM resolver...")

	positions, err := database.GetOpenPositions(s.db)
	if err != nil {
		return fmt.Errorf("failed to get open positions: %w", err)
	}
	if len(positions) == 0 {
		utils.Logger.Info("No open positions to validate")
		return nil
	}

	resolver := s.factory.GetIEMResolver()
	exited := 0

	for _, pos := range positions {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		market, err := s.client.GetMarketByID(pos.MarketID)
		if err != nil {
			utils.Logger.Warnf("ValidatePositions: cannot fetch market %s: %v", pos.MarketID, err)
			continue
		}
		if market.Closed {
			continue
		}

		outcome, confidence, err := resolver.CheckResolution(*market)
		if err != nil {
			utils.Logger.Warnf("ValidatePositions: resolver error for %q: %v — keeping position", pos.MarketQuestion, err)
			continue
		}

		var exitReason string
		shouldExit := false

		if outcome == nil {
			// Pre-peak — resolver can't confirm yet. Keep the position.
			// We only exit when the resolver ACTIVELY returns the opposite outcome.
			utils.Logger.Debugf("ValidatePositions: %q pre-peak, holding %s", pos.MarketQuestion, pos.Outcome)
		} else if !strings.EqualFold(*outcome, pos.Outcome) {
			exitReason = fmt.Sprintf("resolver now says %s (we hold %s, confidence=%.0f%%)", *outcome, pos.Outcome, confidence*100)
			shouldExit = true
		}

		if !shouldExit {
			utils.Logger.Infof("✅ Position valid: %q → %s confirmed", pos.MarketQuestion, pos.Outcome)
			continue
		}

		utils.Logger.Warnf("⚠️  BAD POSITION: %q (holding %s) — %s", pos.MarketQuestion, pos.Outcome, exitReason)

		currentPrice := s.getCurrentPrice(*market, pos.Outcome)
		if currentPrice <= 0 {
			utils.Logger.Warnf("Cannot exit %q — no valid sell price", pos.MarketQuestion)
			continue
		}
		if pos.TokenID == "" {
			utils.Logger.Warnf("Cannot exit %q — no TokenID stored", pos.MarketQuestion)
			continue
		}

		shares := pos.PositionSize / pos.EntryPrice
		sellAmount := shares * currentPrice
		if err := s.client.PlaceSellOrder(pos.TokenID, currentPrice, sellAmount); err != nil {
			utils.Logger.Errorf("Failed to exit bad position %q: %v", pos.MarketQuestion, err)
			continue
		}

		profit := sellAmount - pos.PositionSize
		utils.Logger.Warnf("🚪 Exited bad position: %q | entry=$%.2f exit=$%.2f | P&L=$%.2f",
			pos.MarketQuestion, pos.EntryPrice, currentPrice, profit)

		trade := database.Trade{
			MarketID:       pos.MarketID,
			MarketQuestion: pos.MarketQuestion,
			Outcome:        pos.Outcome,
			EntryPrice:     pos.EntryPrice,
			ExitPrice:      currentPrice,
			Profit:         profit,
			Status:         "closed",
		}
		database.LogTrade(s.db, trade)
		database.ClaimPosition(s.db, pos.ID, currentPrice)

		exited++
	}

	if exited > 0 {
		utils.Logger.Warnf("🚨 Exited %d bad positions", exited)
	} else {
		utils.Logger.Info("✅ All open positions validated — none need exiting")
	}
	return nil
}
