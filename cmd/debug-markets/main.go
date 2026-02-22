package main

import (
	"fmt"
	"strings"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/internal/core"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
)

func main() {
	fmt.Println("🔍 Debug: Market Categorization Test")
	fmt.Println("=====================================\n")

	// Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		return
	}

	// Setup logger
	logger := utils.SetupLogger(cfg.LogLevel)

	// Initialize client
	client := polymarket.NewClient(cfg)
	logger.Info("Fetching active markets...")

	// Get all active markets
	markets, err := client.GetActiveMarkets()
	if err != nil {
		logger.Errorf("Failed to get markets: %v", err)
		return
	}

	fmt.Printf("Total active markets: %d\n\n", len(markets))

	// Initialize monitor for categorization
	monitor := core.NewMonitor(client, cfg)

	// Categorize all markets
	categoryCounts := make(map[string]int)
	weatherMarkets := []polymarket.Market{}

	for _, market := range markets {
		category := monitor.CategorizeMarket(market)
		categoryCounts[category]++

		if category == "weather" {
			weatherMarkets = append(weatherMarkets, market)
		}
	}

	// Print category breakdown
	fmt.Println("📊 Category Breakdown:")
	for category, count := range categoryCounts {
		fmt.Printf("  %s: %d\n", category, count)
	}
	fmt.Println()

	// Show sample weather markets
	fmt.Printf("🌤️  Weather Markets Found: %d\n", len(weatherMarkets))
	if len(weatherMarkets) > 0 {
		fmt.Println("\nSample Weather Markets:")
		for i, market := range weatherMarkets {
			if i >= 10 {
				fmt.Printf("  ... and %d more\n", len(weatherMarkets)-10)
				break
			}
			fmt.Printf("\n  [%d] %s\n", i+1, market.Question)
			fmt.Printf("      Category: %s | Active: %v\n", market.Category, market.Active)
			fmt.Printf("      Resolution: %s\n", market.ResolutionTimestamp)
		}
	} else {
		fmt.Println("\n❌ NO WEATHER MARKETS FOUND!")
		fmt.Println("\nLet's check what questions contain weather keywords:")

		weatherKeywords := []string{"rain", "temperature", "snow", "weather", "sunny", "cloudy", "storm", "temp", "degrees", "fahrenheit", "celsius"}

		for _, keyword := range weatherKeywords {
			count := 0
			for _, market := range markets {
				if strings.Contains(strings.ToLower(market.Question), keyword) {
					if count == 0 {
						fmt.Printf("\nKeyword '%s' found in:\n", keyword)
					}
					if count < 3 {
						fmt.Printf("  - %s\n", market.Question)
					}
					count++
				}
			}
			if count > 3 {
				fmt.Printf("  ... and %d more\n", count-3)
			}
		}
	}
}
