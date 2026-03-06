package parity_arbitrage

import (
	"context"
	"fmt"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/internal/core"
	"github.com/djbro/oracle-weather/internal/database"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	bolt "go.etcd.io/bbolt"
)

// ParityArbitrageStrategy implements parity arbitrage trading
type ParityArbitrageStrategy struct {
	client           *polymarket.PolymarketClient
	db               *bolt.DB
	config           *config.Config
	scanner          *OpportunityScanner
	profitCalc       *ProfitabilityCalculator
	executor         *TradeExecutor
	circuitBreaker   *core.CircuitBreaker
	executedTrades   map[string]time.Time // Track executed trades
	activePositions  int
	maxPositions     int
	scanInterval     time.Duration
	enabled          bool
	// PHASE 8 Enhancements
	priorityScorer   *PriorityScorer
	perfTracker      *PerformanceTracker
}

// NewParityArbitrageStrategy creates a new parity arbitrage strategy
func NewParityArbitrageStrategy(
	client *polymarket.PolymarketClient,
	db *bolt.DB,
	config *config.Config,
	circuitBreaker *core.CircuitBreaker,
) *ParityArbitrageStrategy {
	// Get parity-specific config
	minProfitThreshold := 0.03 // 3% minimum
	minSpread := 0.05          // 5 cents minimum
	maxPositions := 5          // Max 5 concurrent positions
	scanInterval := 30 * time.Second

	// TODO: Read from config if available

	// Create components
	profitCalc := NewProfitabilityCalculator(minProfitThreshold, minSpread)
	scanner := NewOpportunityScanner(client, config, profitCalc)
	executor := NewTradeExecutor(client)

	// PHASE 8 Enhancements
	priorityScorer := NewPriorityScorer()
	perfTracker := NewPerformanceTracker()

	return &ParityArbitrageStrategy{
		client:          client,
		db:              db,
		config:          config,
		scanner:         scanner,
		profitCalc:      profitCalc,
		executor:        executor,
		circuitBreaker:  circuitBreaker,
		executedTrades:  make(map[string]time.Time),
		activePositions: 0,
		maxPositions:    maxPositions,
		scanInterval:    scanInterval,
		enabled:         true, // TODO: Read from config
		priorityScorer:  priorityScorer,
		perfTracker:     perfTracker,
	}
}

// Run starts the parity arbitrage strategy
func (s *ParityArbitrageStrategy) Run(ctx context.Context) error {
	if !s.enabled {
		utils.Logger.Info("Parity arbitrage strategy is disabled")
		return nil
	}

	utils.Logger.Info("🚀 Starting parity arbitrage strategy")
	utils.Logger.Infof("   Scan interval: %s", s.scanInterval)
	utils.Logger.Infof("   Min profit threshold: %.1f%%", s.profitCalc.minProfitThreshold*100)
	utils.Logger.Infof("   Max concurrent positions: %d", s.maxPositions)

	ticker := time.NewTicker(s.scanInterval)
	defer ticker.Stop()

	// Run immediately on start
	if err := s.scanAndExecute(ctx); err != nil {
		utils.Logger.Errorf("Initial scan failed: %v", err)
	}

	// PHASE 8: Log performance report every hour
	reportTicker := time.NewTicker(1 * time.Hour)
	defer reportTicker.Stop()

	// Then run on ticker
	for {
		select {
		case <-ctx.Done():
			// Log final performance report
			s.logPerformanceReport()
			utils.Logger.Info("Parity arbitrage strategy stopped")
			return ctx.Err()

		case <-ticker.C:
			if err := s.scanAndExecute(ctx); err != nil {
				utils.Logger.Errorf("Scan failed: %v", err)
			}

		case <-reportTicker.C:
			// Log hourly performance report
			s.logPerformanceReport()
		}
	}
}

// logPerformanceReport logs a performance summary
func (s *ParityArbitrageStrategy) logPerformanceReport() {
	stats := s.perfTracker.GetOverallStats()

	if stats.TotalTrades == 0 {
		utils.Logger.Info("📊 Parity Performance: No trades executed yet")
		return
	}

	utils.Logger.Info("📊 Parity Arbitrage Performance Report")
	utils.Logger.Infof("   Total Trades: %d (Win Rate: %.1f%%)",
		stats.TotalTrades, stats.WinRate*100)
	utils.Logger.Infof("   Total Profit: $%.2f (Avg: $%.2f per trade)",
		stats.TotalProfit, stats.AverageProfit)
	utils.Logger.Infof("   Best Trade: $%.2f | Worst Trade: $%.2f",
		stats.BestProfit, stats.WorstProfit)
	utils.Logger.Infof("   Running Time: %s", stats.RunningTime.String())

	// Log best time to trade
	bestHour := s.perfTracker.GetBestHourToTrade()
	utils.Logger.Infof("   Best Hour: %02d:00", bestHour)
}

// scanAndExecute performs one scan and executes opportunities
func (s *ParityArbitrageStrategy) scanAndExecute(ctx context.Context) error {
	// Check circuit breaker
	canTrade, reason := s.circuitBreaker.CanTrade()
	if !canTrade {
		utils.Logger.Warnf("🚨 Circuit breaker tripped: %s", reason)
		return fmt.Errorf("circuit breaker tripped: %s", reason)
	}

	// Scan for opportunities
	opportunities, err := s.scanner.ScanForOpportunities(ctx)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	if len(opportunities) == 0 {
		utils.Logger.Debug("No parity opportunities found")
		return nil
	}

	utils.Logger.Infof("🎯 Found %d parity opportunities", len(opportunities))

	// Execute top opportunities (up to available position slots)
	availableSlots := s.maxPositions - s.activePositions
	if availableSlots <= 0 {
		utils.Logger.Warnf("⚠️  No available position slots (%d/%d active)",
			s.activePositions, s.maxPositions)
		return nil
	}

	// PHASE 8: Use priority scoring to rank opportunities
	utils.Logger.Debug("Ranking opportunities by priority score...")
	rankedOpportunities := s.priorityScorer.GetTopOpportunities(opportunities, availableSlots)

	if len(rankedOpportunities) > 0 {
		utils.Logger.Infof("🎖️  Top opportunity score: %.1f/100",
			s.priorityScorer.ScoreOpportunity(rankedOpportunities[0]))
	}

	executed := 0
	for _, opp := range rankedOpportunities {
		// Check if already executed recently
		if s.wasRecentlyExecuted(opp.MarketID) {
			utils.Logger.Debugf("Skipping %s - recently executed", opp.Question)
			continue
		}

		// Execute opportunity
		if err := s.executeOpportunity(ctx, opp); err != nil {
			utils.Logger.Errorf("Failed to execute opportunity: %v", err)
			continue
		}

		executed++
	}

	if executed > 0 {
		utils.Logger.Infof("✅ Successfully executed %d/%d opportunities", executed, len(opportunities))
	}

	return nil
}

// executeOpportunity executes a single parity opportunity
func (s *ParityArbitrageStrategy) executeOpportunity(ctx context.Context, opp ParityOpportunity) error {
	startTime := time.Now()

	// Check balance
	requiredAmount := opp.PositionSize
	if opp.Type == ShortArbitrage {
		requiredAmount = opp.PositionSize * 1.0 // Need $1.00 per position to create split
	}

	hasBalance, err := s.executor.CheckBalance(requiredAmount)
	if err != nil || !hasBalance {
		return fmt.Errorf("insufficient balance: %w", err)
	}

	// Execute trade
	result, err := s.executor.ExecuteOpportunity(ctx, opp)
	executionTime := time.Since(startTime)

	// PHASE 8: Record in performance tracker
	actualProfit := 0.0
	if result.Success {
		// Calculate actual profit (will be updated when market resolves)
		actualProfit = opp.ExpectedProfit // Estimated for now
	}

	tradeRecord := TradeRecord{
		Timestamp:      time.Now(),
		MarketID:       opp.MarketID,
		Question:       opp.Question,
		Category:       "parity", // Could categorize by market type later
		Type:           opp.Type,
		ExpectedProfit: opp.ExpectedProfit,
		ActualProfit:   actualProfit,
		Success:        result.Success,
		ExecutionTime:  executionTime,
	}
	s.perfTracker.RecordTrade(tradeRecord)

	// Log execution summary
	if result.Success {
		utils.Logger.Infof("📊 Trade recorded: Expected $%.2f, Execution time: %s",
			opp.ExpectedProfit, executionTime)
	}

	// Record execution
	if result.Success {
		s.recordExecution(opp, result)
		s.activePositions++

		// Log to database
		if err := s.logToDatabase(opp, result); err != nil {
			utils.Logger.Errorf("Failed to log trade: %v", err)
		}

		// Record in circuit breaker (with 0 profit for now)
		if err := s.circuitBreaker.RecordTrade(0); err != nil {
			utils.Logger.Errorf("Circuit breaker error: %v", err)
		}
	}

	return err
}

// wasRecentlyExecuted checks if market was traded recently (prevent duplicates)
func (s *ParityArbitrageStrategy) wasRecentlyExecuted(marketID string) bool {
	if execTime, exists := s.executedTrades[marketID]; exists {
		// Allow re-execution after 1 hour
		if time.Since(execTime) < 1*time.Hour {
			return true
		}
	}
	return false
}

// recordExecution records an executed trade
func (s *ParityArbitrageStrategy) recordExecution(opp ParityOpportunity, result *ExecutionResult) {
	s.executedTrades[opp.MarketID] = time.Now()

	// Clean up old entries (>24 hours)
	for marketID, execTime := range s.executedTrades {
		if time.Since(execTime) > 24*time.Hour {
			delete(s.executedTrades, marketID)
		}
	}
}

// logToDatabase logs the opportunity and trade to database
func (s *ParityArbitrageStrategy) logToDatabase(opp ParityOpportunity, result *ExecutionResult) error {
	// Log as a parity trade in the database
	// For now, use existing database structure with a marker in the question

	trade := database.Trade{
		MarketID:       opp.MarketID,
		MarketQuestion: "[PARITY] " + opp.Question,
		Outcome:        string(opp.Type), // "LONG" or "SHORT"
		EntryPrice:     opp.PriceSum,
		ExitPrice:      0, // Will be updated when closed
		Profit:         opp.ExpectedProfit,
		Status:         "OPEN",
	}

	return database.LogTrade(s.db, trade)
}

// GetStats returns current strategy statistics
func (s *ParityArbitrageStrategy) GetStats() map[string]interface{} {
	// Get performance stats
	overallStats := s.perfTracker.GetOverallStats()

	return map[string]interface{}{
		"enabled":          s.enabled,
		"active_positions": s.activePositions,
		"max_positions":    s.maxPositions,
		"scan_interval":    s.scanInterval.String(),
		"min_profit":       fmt.Sprintf("%.1f%%", s.profitCalc.minProfitThreshold*100),
		// Performance stats
		"total_trades":    overallStats.TotalTrades,
		"win_rate":        fmt.Sprintf("%.1f%%", overallStats.WinRate*100),
		"total_profit":    fmt.Sprintf("$%.2f", overallStats.TotalProfit),
		"average_profit":  fmt.Sprintf("$%.2f", overallStats.AverageProfit),
		"best_trade":      fmt.Sprintf("$%.2f", overallStats.BestProfit),
		"running_time":    overallStats.RunningTime.String(),
	}
}

// GetPerformanceReport generates a human-readable performance report
func (s *ParityArbitrageStrategy) GetPerformanceReport() string {
	return s.perfTracker.GenerateReport()
}

// GetBestTimeToTrade returns the hour (0-23) with best historical performance
func (s *ParityArbitrageStrategy) GetBestTimeToTrade() int {
	return s.perfTracker.GetBestHourToTrade()
}
