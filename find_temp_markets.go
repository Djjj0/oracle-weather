package main

import (
	"encoding/json"
	"fmt"
	"strings"

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
	markets, _ := client.GetActiveMarkets()

	fmt.Println("=== SEARCHING FOR ACTUAL TEMPERATURE MARKETS ===\n")

	count := 0
	for i := range markets {
		q := strings.ToLower(markets[i].Question)

		// Look for actual temperature/weather keywords
		isTemp := strings.Contains(q, "temperature") ||
		          strings.Contains(q, "°c") ||
		          strings.Contains(q, "°f") ||
		          strings.Contains(q, "degrees")

		if isTemp {
			count++
			fmt.Printf("\n=== TEMPERATURE MARKET #%d ===\n", count)
			fmt.Printf("ID: %s\n", markets[i].ID)
			fmt.Printf("Question: %s\n", markets[i].Question)

			detailed, err := client.GetMarketByID(markets[i].ID)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			fmt.Printf("Resolver Link: '%s'\n", detailed.ResolverLink)

			if detailed.ResolverLink != "" {
				fmt.Printf("\n✅✅✅ HAS RESOLVER LINK! ✅✅✅\n")
				data, _ := json.MarshalIndent(detailed, "", "  ")
				fmt.Println("\nFull JSON:")
				fmt.Println(string(data))
			} else {
				fmt.Printf("❌ No resolver link in API response\n")
			}

			if count >= 10 {
				break
			}
		}
	}

	fmt.Printf("\n\n=== SUMMARY ===\n")
	fmt.Printf("Found %d temperature/weather markets\n", count)
	if count == 0 {
		fmt.Println("\n⚠️  No active temperature markets found right now.")
		fmt.Println("This means we'll need to scrape the Polymarket website HTML to get resolver links.")
	}
}
