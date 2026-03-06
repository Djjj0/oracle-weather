package main

import (
	"fmt"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	utils.Logger = logrus.New()
	utils.Logger.SetLevel(logrus.WarnLevel)

	cfg, _ := config.LoadConfig()
	client := polymarket.NewClient(cfg)

	fmt.Println("=== CHECKING CLOSED MARKETS ===\n")

	markets, err := client.GetRecentClosedMarkets(100)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Total closed markets fetched: %d\n\n", len(markets))

	// Show first 10
	for i, market := range markets {
		if i >= 10 {
			break
		}

		fmt.Printf("%d. %s\n", i+1, market.Question)
		fmt.Printf("   Closed: %v, Resolved: %v\n", market.Closed, market.ResolvedOutcome != nil)
		fmt.Printf("   Updated: %s\n\n", market.UpdatedAt.Format("2006-01-02 15:04"))
	}
}
