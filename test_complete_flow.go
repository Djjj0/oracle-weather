package main

import (
	"fmt"
	"log"
	"regexp"
	
	"github.com/djbro/oracle-weather/pkg/weather"
)

func main() {
	// Open learning database
	learningDB, err := weather.NewLearningDB("./data/learning.db")
	if err != nil {
		log.Fatalf("Failed to open learning DB: %v", err)
	}
	defer learningDB.Close()

	fmt.Println("=== COMPLETE FLOW TEST ===\n")

	// Test URL (Chicago market)
	testURL := "https://www.wunderground.com/history/daily/us/il/chicago/KORD"

	// Step 1: Extract station code
	fmt.Println("STEP 1: Extract station code from WU URL")
	fmt.Printf("URL: %s\n", testURL)

	stationRegex := regexp.MustCompile(`/([A-Z]{4})$`)
	matches := stationRegex.FindStringSubmatch(testURL)
	if len(matches) < 2 {
		log.Fatal("Could not extract station code")
	}
	stationCode := matches[1]
	fmt.Printf("✅ Station code: %s\n\n", stationCode)

	// Step 2: Look up optimal entry time in learning DB
	fmt.Println("STEP 2: Look up optimal entry time in learning database")
	optimalHour, timezone, err := learningDB.GetOptimalEntryByStation(stationCode)
	if err != nil {
		log.Fatalf("Failed to lookup station: %v", err)
	}

	city, err := learningDB.GetCityByStation(stationCode)
	if err != nil {
		log.Fatalf("Failed to get city: %v", err)
	}

	fmt.Printf("✅ Found data for station %s:\n", stationCode)
	fmt.Printf("   City: %s\n", city)
	fmt.Printf("   Timezone: %s\n", timezone)
	fmt.Printf("   Optimal entry hour: %.2f (%.0f:%02.0f %s time)\n\n",
		optimalHour,
		float64(int(optimalHour)),
		(optimalHour-float64(int(optimalHour)))*60,
		timezone)

	// Step 3: Get city stats for more details
	fmt.Println("STEP 3: Get detailed city statistics")
	stats, err := learningDB.GetCityStats(city)
	if err != nil {
		log.Fatalf("Failed to get city stats: %v", err)
	}

	fmt.Printf("✅ City: %s\n", stats.City)
	fmt.Printf("   Total markets analyzed: %d\n", stats.TotalMarkets)
	fmt.Printf("   Avg high temp time: %.2f (%.0f:%02.0f)\n",
		stats.AvgHighTempHour,
		float64(int(stats.AvgHighTempHour)),
		(stats.AvgHighTempHour-float64(int(stats.AvgHighTempHour)))*60)
	fmt.Printf("   Avg market resolution time: %.2f (%.0f:%02.0f)\n",
		stats.AvgMarketResolutionHour,
		float64(int(stats.AvgMarketResolutionHour)),
		(stats.AvgMarketResolutionHour-float64(int(stats.AvgMarketResolutionHour)))*60)
	fmt.Printf("   Optimal entry hour: %.2f (%.0f:%02.0f)\n",
		stats.OptimalEntryHour,
		float64(int(stats.OptimalEntryHour)),
		(stats.OptimalEntryHour-float64(int(stats.OptimalEntryHour)))*60)
	fmt.Printf("   Success rate: %.1f%%\n", stats.SuccessRate*100)
	fmt.Printf("   Confidence score: %.2f\n\n", stats.ConfidenceScore)

	fmt.Println("=== RECOMMENDATION ===")
	fmt.Printf("For %s markets using station %s:\n", city, stationCode)
	fmt.Printf("- Wait until %.0f:%02.0f %s time before checking resolution\n",
		float64(int(optimalHour)),
		(optimalHour-float64(int(optimalHour)))*60,
		timezone)
	fmt.Printf("- This is based on %d historical markets\n", stats.TotalMarkets)
	fmt.Printf("- Historical success rate: %.1f%%\n", stats.SuccessRate*100)
}
