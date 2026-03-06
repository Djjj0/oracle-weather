package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	utils.Logger = logrus.New()
	utils.Logger.SetLevel(logrus.WarnLevel)

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	client := polymarket.NewClient(cfg)
	markets, err := client.GetActiveMarkets()
	if err != nil {
		log.Fatalf("Failed to fetch markets: %v", err)
	}

	fmt.Println("=== SEARCHING FOR WEATHER MARKETS ===\n")

	count := 0
	for i := range markets {
		q := strings.ToLower(markets[i].Question)

		// Look for weather-related markets
		if strings.Contains(q, "temperature") ||
		   strings.Contains(q, "highest") ||
		   strings.Contains(q, "weather") ||
		   strings.Contains(q, "rain") {

			count++
			fmt.Printf("\n=== WEATHER MARKET #%d ===\n", count)
			fmt.Printf("ID: %s\n", markets[i].ID)
			fmt.Printf("Question: %s\n", markets[i].Question)

			// Get detailed info
			detailed, err := client.GetMarketByID(markets[i].ID)
			if err != nil {
				fmt.Printf("Error fetching details: %v\n", err)
				continue
			}

			// Show resolver link
			fmt.Printf("Resolver Link: '%s'\n", detailed.ResolverLink)

			if detailed.ResolverLink != "" {
				fmt.Printf("✅ HAS RESOLVER LINK!\n")
			} else {
				fmt.Printf("❌ No resolver link\n")
			}

			// Show description excerpt
			if len(detailed.Description) > 0 {
				desc := detailed.Description
				if len(desc) > 200 {
					desc = desc[:200] + "..."
				}
				fmt.Printf("Description: %s\n", desc)
			}

			// Show full JSON for first 3 weather markets
			if count <= 3 {
				data, _ := json.MarshalIndent(detailed, "", "  ")
				fmt.Println("\nFull JSON:")
				fmt.Println(string(data))
			}

			if count >= 10 {
				break
			}
		}
	}

	fmt.Printf("\n\n=== SUMMARY ===\n")
	fmt.Printf("Found %d weather-related markets\n", count)
}
