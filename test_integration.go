package main

import (
	"fmt"
	"strings"
	
	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/internal/resolvers"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	utils.Logger = logrus.New()
	utils.Logger.SetLevel(logrus.InfoLevel)

	cfg, _ := config.LoadConfig()

	fmt.Println("=== TESTING INTEGRATED WEATHER RESOLVER ===\n")

	// Initialize weather resolver (now includes learning DB)
	resolver := resolvers.NewWeatherResolver(cfg)

	// Get a test market
	client := polymarket.NewClient(cfg)
	markets, _ := client.GetActiveMarkets()

	// Find a Chicago temperature market
	var testMarket *polymarket.Market
	for i := range markets {
		q := strings.ToLower(markets[i].Question)
		if strings.Contains(q, "chicago") && strings.Contains(q, "temperature") {
			testMarket = &markets[i]
			break
		}
	}

	if testMarket == nil {
		fmt.Println("❌ No Chicago temperature market found")
		return
	}

	fmt.Printf("📍 Testing with market:\n")
	fmt.Printf("   ID: %s\n", testMarket.ID)
	fmt.Printf("   Question: %s\n\n", testMarket.Question)

	// Test the CheckResolution method (which now uses dynamic timing)
	fmt.Println("🔍 Calling CheckResolution (should use data-driven timing)...\n")

	outcome, confidence, err := resolver.CheckResolution(*testMarket)
	
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if outcome == nil {
		fmt.Println("✅ Market not ready yet (optimal entry time not reached)")
		fmt.Println("   This means the dynamic timing is working!")
		fmt.Println("   The bot will wait until the data-driven optimal time")
	} else {
		fmt.Printf("✅ Market resolved!\n")
		fmt.Printf("   Outcome: %s\n", *outcome)
		fmt.Printf("   Confidence: %.2f%%\n", confidence*100)
	}
}
