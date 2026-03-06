package main

import (
	"fmt"
	"time"
	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
)

func main() {
	cfg := &config.Config{
		PolymarketAPIKey: "",
		MinProfitThreshold: 0.05,
		MaxPositionSize: 100,
	}
	
	client := polymarket.NewOfficialClient(cfg)
	
	fmt.Println("=== FETCHING REAL POLYMARKET WEATHER MARKETS ===\n")
	
	markets, err := client.GetActiveMarkets()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	
	fmt.Printf("Total active markets found: %d\n\n", len(markets))
	
	// Find weather markets
	weatherCount := 0
	resolvedCount := 0
	todayCount := 0
	
	now := time.Now()
	
	for _, m := range markets {
		if contains(m.Question, "temperature") || contains(m.Question, "rain") || contains(m.Question, "weather") {
			weatherCount++
			
			// Check if for today
			if contains(m.Question, "February 23") || contains(m.Question, "Feb 23") {
				todayCount++
			}
			
			// Show first few
			if weatherCount <= 5 {
				fmt.Printf("%d. %s\n", weatherCount, m.Question)
				if m.EndDate != "" {
					fmt.Printf("   End Date: %s\n", m.EndDate)
				}
			}
		}
	}
	
	fmt.Printf("\n📊 Summary:\n")
	fmt.Printf("   Weather markets: %d\n", weatherCount)
	fmt.Printf("   Markets for today (Feb 23): %d\n", todayCount)
	fmt.Printf("   Current time: %s\n", now.Format("2006-01-02 15:04:05"))
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && 
		(s == substr || len(s) >= len(substr) && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		findSubstr(s, substr)))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
