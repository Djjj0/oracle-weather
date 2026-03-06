// Package main implements a backtest tool that replays historical closed positions
// against different MIN_PROFIT_THRESHOLD settings to project win rate and expected profit.
package main

import (
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/joho/godotenv"
	bolt "go.etcd.io/bbolt"

	"github.com/djbro/oracle-weather/internal/database"
)

// thresholdResult holds backtest metrics for a single threshold value.
type thresholdResult struct {
	Threshold  float64
	Trades     int
	Wins       int
	Losses     int
	WinRate    float64
	TotalPnL   float64
	AvgPerTrade float64
}

func main() {
	// Load .env if present (best-effort; ignore error if file missing)
	_ = godotenv.Load()

	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "./data/trades.db"
	}

	// Open database read-only
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to open database at %s: %v\n", dbPath, err)
		os.Exit(1)
	}
	defer db.Close()

	// Load all closed positions
	positions, err := database.GetAllClosedPositions(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to load closed positions: %v\n", err)
		os.Exit(1)
	}

	if len(positions) == 0 {
		fmt.Println("No closed positions found in the database.")
		fmt.Println("Positions must have status CLAIMED or LOST to be included in the backtest.")
		os.Exit(0)
	}

	fmt.Printf("Loaded %d closed positions from %s\n\n", len(positions), dbPath)

	// Thresholds to test
	thresholds := []float64{0.01, 0.02, 0.03, 0.04, 0.05, 0.07, 0.10}

	results := make([]thresholdResult, 0, len(thresholds))

	for _, threshold := range thresholds {
		r := thresholdResult{Threshold: threshold}

		for _, pos := range positions {
			// Entry edge = expected payout minus entry cost.
			// Buying a YES share at EntryPrice costs EntryPrice; if correct it pays $1.
			// Edge = 1.0 - EntryPrice (ignoring fees for threshold filtering,
			// consistent with how the live bot evaluates opportunities).
			edge := 1.0 - pos.EntryPrice
			if edge < threshold {
				continue
			}

			r.Trades++
			if pos.Status == "CLAIMED" {
				r.Wins++
				r.TotalPnL += pos.Profit
			} else {
				// LOST
				r.Losses++
				r.TotalPnL += pos.Profit // Profit is negative (= -PositionSize)
			}
		}

		if r.Trades > 0 {
			r.WinRate = float64(r.Wins) / float64(r.Trades) * 100.0
			r.AvgPerTrade = r.TotalPnL / float64(r.Trades)
		}

		results = append(results, r)
	}

	// Print comparison table
	fmt.Println("Backtest Results — Historical Closed Positions vs. MIN_PROFIT_THRESHOLD")
	fmt.Println("=========================================================================")
	fmt.Printf("%-10s | %-6s | %-4s | %-7s | %-9s | %-10s | %-10s\n",
		"Threshold", "Trades", "Wins", "Losses", "Win Rate", "Total P&L", "Avg/Trade")
	fmt.Println("-----------+--------+------+---------+-----------+------------+------------")

	for _, r := range results {
		pnlSign := "+"
		if r.TotalPnL < 0 {
			pnlSign = ""
		}
		avgSign := "+"
		if r.AvgPerTrade < 0 {
			avgSign = ""
		}
		fmt.Printf("%-10s | %-6d | %-4d | %-7d | %-9s | %s$%-9.2f | %s$%.2f\n",
			fmt.Sprintf("%d%%", int(math.Round(r.Threshold*100))),
			r.Trades,
			r.Wins,
			r.Losses,
			fmt.Sprintf("%.1f%%", r.WinRate),
			pnlSign, r.TotalPnL,
			avgSign, r.AvgPerTrade,
		)
	}

	fmt.Println()

	// Recommend optimal threshold: highest total P&L among those with win rate > 80%
	// and at least 1 trade.
	recommendation := findOptimal(results)
	if recommendation == nil {
		fmt.Println("Recommendation: Not enough data to make a recommendation.")
		fmt.Println("  Need at least one threshold with win rate > 80% and at least 1 trade.")
	} else {
		fmt.Printf("Recommendation: Set MIN_PROFIT_THRESHOLD=%.2f (%.0f%%)\n",
			recommendation.Threshold, recommendation.Threshold*100)
		fmt.Printf("  Trades: %d | Win Rate: %.1f%% | Total P&L: $%.2f | Avg/Trade: $%.2f\n",
			recommendation.Trades, recommendation.WinRate,
			recommendation.TotalPnL, recommendation.AvgPerTrade)
		fmt.Println("  (Highest total P&L among thresholds with win rate > 80%)")
	}
}

// findOptimal returns the threshold result with the best risk-adjusted return:
// highest total P&L where win rate > 80% and at least 1 trade.
func findOptimal(results []thresholdResult) *thresholdResult {
	// Filter eligible results
	var eligible []thresholdResult
	for _, r := range results {
		if r.Trades > 0 && r.WinRate > 80.0 {
			eligible = append(eligible, r)
		}
	}

	if len(eligible) == 0 {
		return nil
	}

	// Sort by TotalPnL descending
	sort.Slice(eligible, func(i, j int) bool {
		return eligible[i].TotalPnL > eligible[j].TotalPnL
	})

	best := eligible[0]
	return &best
}
