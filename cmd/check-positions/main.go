package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/internal/database"
	"github.com/djbro/oracle-weather/pkg/polymarket"
)

func main() {
	cfg, _ := config.LoadConfig()
	db, err := database.InitDB(cfg.DatabasePath)
	if err != nil {
		fmt.Println("DB error:", err)
		os.Exit(1)
	}
	defer db.Close()

	positions, err := database.GetOpenPositions(db)
	if err != nil {
		fmt.Println("Positions error:", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d open positions\n\n", len(positions))

	client := polymarket.NewClient(cfg)
	for _, pos := range positions {
		market, err := client.GetMarketByID(pos.MarketID)
		if err != nil {
			fmt.Printf("ERROR fetching market %s: %v\n\n", pos.MarketID, err)
			continue
		}
		b, _ := json.MarshalIndent(map[string]interface{}{
			"question":         market.Question,
			"active":           market.Active,
			"closed":           market.Closed,
			"resolved_outcome": market.ResolvedOutcome,
			"end_date":         market.ResolutionTimestamp,
			"prices":           market.Prices,
		}, "", "  ")
		fmt.Printf("Position: %s\nOutcome held: %s | Entry: $%.2f\n%s\n\n",
			pos.MarketQuestion, pos.Outcome, pos.EntryPrice, string(b))
	}
}
