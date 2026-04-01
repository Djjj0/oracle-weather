// pnl-report prints a per-trade P&L table from BoltDB.
//
// Usage:
//
//	go run ./cmd/pnl-report/            (last 7 days)
//	go run ./cmd/pnl-report/ --days 30
//	go run ./cmd/pnl-report/ --all
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/internal/database"
)

func main() {
	days := flag.Int("days", 7, "Number of past days to show")
	all  := flag.Bool("all",  false, "Show all-time history")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR loading config: %v\n", err)
		os.Exit(1)
	}

	db, err := database.InitDB(cfg.DatabasePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	positions, err := database.GetAllClosedPositions(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR reading closed positions: %v\n", err)
		os.Exit(1)
	}

	open, err := database.GetOpenPositions(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN reading open positions: %v\n", err)
	}

	// Filter to the requested date window
	var filtered []database.Position
	cutoff := time.Now().AddDate(0, 0, -*days)
	for _, p := range positions {
		if *all || p.ClaimedAt.After(cutoff) {
			filtered = append(filtered, p)
		}
	}

	// Header
	fmt.Printf("%-10s | %-40s | %-7s | %-6s | %-6s | %s\n",
		"DATE", "MARKET", "OUTCOME", "COST", "ENTRY", "PNL")
	fmt.Println("───────────|──────────────────────────────────────────|─────────|────────|────────|────────")

	var totalPnL float64
	wins, losses := 0, 0
	for _, p := range filtered {
		dateStr := p.ClaimedAt.Format("2006-01-02")
		question := p.MarketQuestion
		if len(question) > 40 {
			question = question[:37] + "..."
		}
		pnlSign := "+"
		if p.Profit < 0 {
			pnlSign = ""
		}
		fmt.Printf("%-10s | %-40s | %-7s | $%-5.2f | $%-5.2f | %s$%.2f\n",
			dateStr, question, p.Outcome, p.PositionSize, p.EntryPrice, pnlSign, p.Profit)
		totalPnL += p.Profit
		if p.Profit > 0 {
			wins++
		} else {
			losses++
		}
	}

	fmt.Println("───────────────────────────────────────────────────────────────────────────────────────────")

	// Summary line for the requested window
	windowLabel := fmt.Sprintf("LAST %d DAYS", *days)
	if *all {
		windowLabel = "ALL TIME"
	}
	pnlSign := "+"
	if totalPnL < 0 {
		pnlSign = ""
	}
	fmt.Printf("%-11s | %d trades, %d wins, %d losses | %s$%.2f\n",
		windowLabel, len(filtered), wins, losses, pnlSign, totalPnL)

	// All-time totals when showing a subset
	if !*all {
		var allTimePnL float64
		allWins, allLosses := 0, 0
		for _, p := range positions {
			allTimePnL += p.Profit
			if p.Profit > 0 {
				allWins++
			} else {
				allLosses++
			}
		}
		atSign := "+"
		if allTimePnL < 0 {
			atSign = ""
		}
		fmt.Printf("%-11s | %d trades, %d wins, %d losses | %s$%.2f\n",
			"ALL TIME", len(positions), allWins, allLosses, atSign, allTimePnL)
	}

	// Open positions
	if len(open) > 0 {
		fmt.Printf("\nOpen positions: %d\n", len(open))
		for _, p := range open {
			fmt.Printf("  • %-40s → %-5s @ $%.2f, cost $%.2f, entered %s\n",
				truncate(p.MarketQuestion, 40), p.Outcome, p.EntryPrice, p.PositionSize,
				p.EntryTime.Format("2006-01-02 15:04"))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
