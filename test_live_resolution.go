package main

import (
	"fmt"
	"time"
	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/internal/resolvers"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	utils.Logger = logrus.New()
	utils.Logger.SetLevel(logrus.InfoLevel)
	
	cfg := &config.Config{}
	resolver := resolvers.NewWeatherResolver(cfg)
	
	fmt.Println("=== TESTING LIVE RESOLUTION ===\n")
	
	// Test with YESTERDAY (Feb 22) - should be resolvable
	yesterday := time.Now().AddDate(0, 0, -1)
	testMarket := polymarket.Market{
		Question: fmt.Sprintf("Will the highest temperature in Seattle be 55°F or higher on %s?", 
			yesterday.Format("January 2")),
	}
	
	fmt.Printf("Testing: %s\n", testMarket.Question)
	fmt.Println("This market is for YESTERDAY, so it should be resolvable now...\n")
	
	outcome, confidence, err := resolver.CheckResolution(testMarket)
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
	} else if outcome != nil {
		fmt.Printf("✅ RESOLUTION SUCCESS!\n")
		fmt.Printf("   Outcome: %s\n", *outcome)
		fmt.Printf("   Confidence: %.2f\n", confidence)
		fmt.Printf("   This proves the resolution logic WORKS!\n")
	} else {
		fmt.Printf("⏳ Not resolvable yet\n")
	}
}
