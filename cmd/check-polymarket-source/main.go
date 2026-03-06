package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize logger
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

	fmt.Println("=== POLYMARKET WEATHER MARKET DATA SOURCE INVESTIGATION ===")
	fmt.Println("Checking market descriptions for resolution source mentions")
	fmt.Println()

	// Look for weather markets
	weatherMarkets := 0
	sourcesMentioned := make(map[string]int)

	for _, market := range markets {
		q := strings.ToLower(market.Question)

		isWeather := strings.Contains(q, "temperature") ||
			strings.Contains(q, "highest") ||
			strings.Contains(q, "rain") ||
			strings.Contains(q, "wind")

		if !isWeather {
			continue
		}

		weatherMarkets++

		// Check for data source mentions
		fullText := strings.ToLower(market.Question + " " + market.Description)

		// Common source keywords
		sources := map[string][]string{
			"Weather Underground": {"weather underground", "wunderground", "wunder"},
			"NOAA": {"noaa", "national weather service", "nws"},
			"Met Office": {"met office", "metoffice"},
			"Environment Canada": {"environment canada", "ec.gc.ca"},
			"Aviation/METAR": {"metar", "aviation weather", "awc.noaa"},
			"Open-Meteo": {"open-meteo", "openmeteo"},
			"Visual Crossing": {"visual crossing", "visualcrossing"},
		}

		for sourceName, keywords := range sources {
			for _, keyword := range keywords {
				if strings.Contains(fullText, keyword) {
					sourcesMentioned[sourceName]++
					if weatherMarkets <= 10 {
						fmt.Printf("✅ Found mention: %s\n", sourceName)
						fmt.Printf("   Market: %s\n", market.Question)
						fmt.Printf("   Description excerpt: %s\n\n", truncate(market.Description, 150))
					}
				}
			}
		}

		// Show first 10 weather markets regardless
		if weatherMarkets <= 10 {
			fmt.Printf("Market %d: %s\n", weatherMarkets, market.Question)
			if market.Description != "" {
				fmt.Printf("Description: %s\n", truncate(market.Description, 200))
			}
			fmt.Println()
		}
	}

	fmt.Println("\n=== SUMMARY ===")
	fmt.Printf("Total weather markets found: %d\n\n", weatherMarkets)

	if len(sourcesMentioned) > 0 {
		fmt.Println("Data sources mentioned:")
		for source, count := range sourcesMentioned {
			fmt.Printf("- %s: %d mentions\n", source, count)
		}
	} else {
		fmt.Println("⚠️  No explicit data source mentions found in market descriptions")
		fmt.Println()
		fmt.Println("This means Polymarket likely uses a standard source without citing it.")
		fmt.Println("Common practice: Weather Underground (most popular for prediction markets)")
	}

	fmt.Println("\n=== RECOMMENDATION ===")
	fmt.Println("1. Check resolved markets to see what temperatures were used")
	fmt.Println("2. Compare against Weather Underground historical data")
	fmt.Println("3. If they match, implement WU scraper")
	fmt.Println("4. If uncertain, ask in Polymarket Discord/support")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
