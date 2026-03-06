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
	utils.Logger.SetLevel(logrus.WarnLevel)

	cfg, _ := config.LoadConfig()

	fmt.Println("=== TESTING RESOLVER LINK SCRAPING ===\n")

	// We know this URL works from our earlier test
	testURL := "https://polymarket.com/event/highest-temperature-in-chicago-on-february-27-2026/highest-temperature-in-chicago-on-february-27-2026-56-57f"
	
	fmt.Printf("Test URL: %s\n\n", testURL)

	// Get markets to find one that matches
	client := polymarket.NewClient(cfg)
	markets, _ := client.GetActiveMarkets()

	var testMarket *polymarket.Market
	for i := range markets {
		q := strings.ToLower(markets[i].Question)
		if strings.Contains(q, "chicago") && 
		   strings.Contains(q, "56") && 
		   strings.Contains(q, "57") &&
		   strings.Contains(q, "february 27") {
			testMarket = &markets[i]
			break
		}
	}

	if testMarket == nil {
		fmt.Println("❌ Specific test market not found")
		fmt.Println("Testing with URL building from question...\n")
		
		// Test question
		testQuestion := "Will the highest temperature in Chicago be 56-57°F on February 27, 2026?"
		fmt.Printf("Question: %s\n", testQuestion)
		
		// This calls our buildSlugFromQuestion function indirectly
		
		// Try to get resolver link
		fmt.Println("\nAttempting to scrape resolver link...")
		// We can't call getResolverLink directly as it's unexported
		// But we can check if CheckResolution works
		
		return
	}

	fmt.Printf("Found market:\n")
	fmt.Printf("  ID: %s\n", testMarket.ID)
	fmt.Printf("  Question: %s\n\n", testMarket.Question)

	// Initialize resolver
	resolver := resolvers.NewWeatherResolver(cfg)

	fmt.Println("Testing CheckResolution (will attempt to scrape resolver link)...\n")
	
	outcome, conf, err := resolver.CheckResolution(*testMarket)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else if outcome == nil {
		fmt.Println("Market not ready yet")
	} else {
		fmt.Printf("Resolved to: %s (%.1f%% confidence)\n", *outcome, conf*100)
	}
}
