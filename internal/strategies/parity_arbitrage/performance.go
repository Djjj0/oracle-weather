package parity_arbitrage

import (
	"fmt"
	"sync"
	"time"
)

// PerformanceTracker tracks historical performance by category
// PHASE 8: Enhancement - Learn which market types are most profitable
type PerformanceTracker struct {
	mu          sync.RWMutex
	categories  map[string]*CategoryPerformance
	allTrades   []TradeRecord
	startTime   time.Time
}

// CategoryPerformance tracks performance for a specific market category
type CategoryPerformance struct {
	Category       string
	TotalTrades    int
	WinningTrades  int
	LosingTrades   int
	TotalProfit    float64
	AverageProfit  float64
	BestProfit     float64
	WorstProfit    float64
	WinRate        float64
	BestTimeOfDay  int // Hour 0-23
	TradesByHour   [24]int
	ProfitByHour   [24]float64
}

// TradeRecord represents a single trade for historical analysis
type TradeRecord struct {
	Timestamp      time.Time
	MarketID       string
	Question       string
	Category       string
	Type           ArbitrageType
	ExpectedProfit float64
	ActualProfit   float64
	Success        bool
	ExecutionTime  time.Duration
}

// NewPerformanceTracker creates a new performance tracker
func NewPerformanceTracker() *PerformanceTracker {
	return &PerformanceTracker{
		categories: make(map[string]*CategoryPerformance),
		allTrades:  make([]TradeRecord, 0),
		startTime:  time.Now(),
	}
}

// RecordTrade records a completed trade
func (pt *PerformanceTracker) RecordTrade(record TradeRecord) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	// Add to all trades
	pt.allTrades = append(pt.allTrades, record)

	// Get or create category performance
	category := record.Category
	if category == "" {
		category = "unknown"
	}

	catPerf, exists := pt.categories[category]
	if !exists {
		catPerf = &CategoryPerformance{
			Category: category,
		}
		pt.categories[category] = catPerf
	}

	// Update category stats
	catPerf.TotalTrades++
	if record.Success && record.ActualProfit > 0 {
		catPerf.WinningTrades++
	} else {
		catPerf.LosingTrades++
	}

	catPerf.TotalProfit += record.ActualProfit
	catPerf.AverageProfit = catPerf.TotalProfit / float64(catPerf.TotalTrades)

	if record.ActualProfit > catPerf.BestProfit {
		catPerf.BestProfit = record.ActualProfit
	}
	if record.ActualProfit < catPerf.WorstProfit || catPerf.WorstProfit == 0 {
		catPerf.WorstProfit = record.ActualProfit
	}

	catPerf.WinRate = float64(catPerf.WinningTrades) / float64(catPerf.TotalTrades)

	// Track by hour
	hour := record.Timestamp.Hour()
	catPerf.TradesByHour[hour]++
	catPerf.ProfitByHour[hour] += record.ActualProfit

	// Update best time of day
	bestHour := 0
	bestProfit := catPerf.ProfitByHour[0]
	for h := 1; h < 24; h++ {
		if catPerf.ProfitByHour[h] > bestProfit {
			bestProfit = catPerf.ProfitByHour[h]
			bestHour = h
		}
	}
	catPerf.BestTimeOfDay = bestHour
}

// GetCategoryPerformance returns performance for a specific category
func (pt *PerformanceTracker) GetCategoryPerformance(category string) *CategoryPerformance {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if catPerf, exists := pt.categories[category]; exists {
		// Return a copy to avoid race conditions
		copy := *catPerf
		return &copy
	}

	return nil
}

// GetAllCategories returns performance for all categories
func (pt *PerformanceTracker) GetAllCategories() map[string]*CategoryPerformance {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	// Return copies
	result := make(map[string]*CategoryPerformance)
	for cat, perf := range pt.categories {
		copy := *perf
		result[cat] = &copy
	}

	return result
}

// GetOverallStats returns overall performance statistics
func (pt *PerformanceTracker) GetOverallStats() OverallStats {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	stats := OverallStats{
		TotalTrades:    len(pt.allTrades),
		TotalProfit:    0,
		AverageProfit:  0,
		BestProfit:     0,
		WorstProfit:    0,
		WinRate:        0,
		WinningTrades:  0,
		LosingTrades:   0,
		RunningTime:    time.Since(pt.startTime),
	}

	if len(pt.allTrades) == 0 {
		return stats
	}

	for _, trade := range pt.allTrades {
		stats.TotalProfit += trade.ActualProfit
		if trade.Success && trade.ActualProfit > 0 {
			stats.WinningTrades++
		} else {
			stats.LosingTrades++
		}

		if trade.ActualProfit > stats.BestProfit {
			stats.BestProfit = trade.ActualProfit
		}
		if trade.ActualProfit < stats.WorstProfit || stats.WorstProfit == 0 {
			stats.WorstProfit = trade.ActualProfit
		}
	}

	stats.AverageProfit = stats.TotalProfit / float64(stats.TotalTrades)
	stats.WinRate = float64(stats.WinningTrades) / float64(stats.TotalTrades)

	return stats
}

// GetRecentTrades returns the N most recent trades
func (pt *PerformanceTracker) GetRecentTrades(n int) []TradeRecord {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if len(pt.allTrades) == 0 {
		return []TradeRecord{}
	}

	start := len(pt.allTrades) - n
	if start < 0 {
		start = 0
	}

	// Return copies
	result := make([]TradeRecord, len(pt.allTrades)-start)
	copy(result, pt.allTrades[start:])
	return result
}

// ShouldScanCategory determines if a category is worth scanning based on history
func (pt *PerformanceTracker) ShouldScanCategory(category string) bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	catPerf, exists := pt.categories[category]
	if !exists {
		// No history yet, allow scanning
		return true
	}

	// Don't scan if win rate < 70% and we have at least 10 trades
	if catPerf.TotalTrades >= 10 && catPerf.WinRate < 0.70 {
		return false
	}

	// Don't scan if average profit is negative
	if catPerf.TotalTrades >= 5 && catPerf.AverageProfit < 0 {
		return false
	}

	return true
}

// GetBestHourToTrade returns the hour (0-23) with best historical performance
func (pt *PerformanceTracker) GetBestHourToTrade() int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	// Aggregate profit by hour across all categories
	totalProfitByHour := [24]float64{}
	for _, catPerf := range pt.categories {
		for h := 0; h < 24; h++ {
			totalProfitByHour[h] += catPerf.ProfitByHour[h]
		}
	}

	// Find best hour
	bestHour := 0
	bestProfit := totalProfitByHour[0]
	for h := 1; h < 24; h++ {
		if totalProfitByHour[h] > bestProfit {
			bestProfit = totalProfitByHour[h]
			bestHour = h
		}
	}

	return bestHour
}

// OverallStats represents overall trading statistics
type OverallStats struct {
	TotalTrades    int
	WinningTrades  int
	LosingTrades   int
	TotalProfit    float64
	AverageProfit  float64
	BestProfit     float64
	WorstProfit    float64
	WinRate        float64
	RunningTime    time.Duration
}

// GenerateReport creates a human-readable performance report
func (pt *PerformanceTracker) GenerateReport() string {
	overall := pt.GetOverallStats()
	categories := pt.GetAllCategories()

	report := "📊 Parity Arbitrage Performance Report\n"
	report += "=====================================\n\n"

	report += fmt.Sprintf("Overall Performance:\n")
	report += fmt.Sprintf("  Total Trades: %d\n", overall.TotalTrades)
	report += fmt.Sprintf("  Win Rate: %.1f%% (%d wins, %d losses)\n",
		overall.WinRate*100, overall.WinningTrades, overall.LosingTrades)
	report += fmt.Sprintf("  Total Profit: $%.2f\n", overall.TotalProfit)
	report += fmt.Sprintf("  Average Profit: $%.2f\n", overall.AverageProfit)
	report += fmt.Sprintf("  Best Trade: $%.2f\n", overall.BestProfit)
	report += fmt.Sprintf("  Worst Trade: $%.2f\n", overall.WorstProfit)
	report += fmt.Sprintf("  Running Time: %s\n\n", overall.RunningTime.String())

	report += "Performance by Category:\n"
	for _, catPerf := range categories {
		if catPerf.TotalTrades > 0 {
			report += fmt.Sprintf("  %s:\n", catPerf.Category)
			report += fmt.Sprintf("    Trades: %d\n", catPerf.TotalTrades)
			report += fmt.Sprintf("    Win Rate: %.1f%%\n", catPerf.WinRate*100)
			report += fmt.Sprintf("    Avg Profit: $%.2f\n", catPerf.AverageProfit)
			report += fmt.Sprintf("    Best Time: %02d:00\n", catPerf.BestTimeOfDay)
		}
	}

	return report
}
