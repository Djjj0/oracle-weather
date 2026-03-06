package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/djbro/oracle-weather/pkg/weather"
)

func main() {
	fmt.Println("=== RECALCULATING CITY STATISTICS ===\n")

	// Initialize learning database
	learningDB, err := weather.NewLearningDB("./data/learning.db")
	if err != nil {
		log.Fatalf("Failed to initialize learning database: %v", err)
	}
	defer learningDB.Close()

	// Get all cities from the database
	cities := []string{"seattle", "chicago", "new york", "miami", "phoenix", "los angeles"}

	fmt.Println("Recalculating statistics for all cities...\n")

	for _, city := range cities {
		fmt.Printf("📊 Processing: %s\n", strings.Title(city))

		if err := learningDB.UpdateCityStats(city); err != nil {
			fmt.Printf("   ❌ Error: %v\n", err)
			continue
		}

		stats, err := learningDB.GetCityStats(city)
		if err != nil {
			fmt.Printf("   ⚠️  Could not retrieve stats: %v\n", err)
			continue
		}

		fmt.Printf("   ✅ Success!\n")
		fmt.Printf("   Data points: %d\n", stats.TotalMarkets)
		fmt.Printf("   Avg high temp time: %02d:%02d\n",
			int(stats.AvgHighTempHour), int((stats.AvgHighTempHour-float64(int(stats.AvgHighTempHour)))*60))
		fmt.Printf("   Avg IEM data final: %02d:%02d\n",
			int(stats.AvgIEMFinalHour), int((stats.AvgIEMFinalHour-float64(int(stats.AvgIEMFinalHour)))*60))
		fmt.Printf("   Avg market resolution: %02d:%02d\n",
			int(stats.AvgMarketResolutionHour), int((stats.AvgMarketResolutionHour-float64(int(stats.AvgMarketResolutionHour)))*60))
		fmt.Printf("   ⭐ OPTIMAL ENTRY TIME: %02d:%02d\n",
			int(stats.OptimalEntryHour), int((stats.OptimalEntryHour-float64(int(stats.OptimalEntryHour)))*60))
		fmt.Printf("   Success rate: %.1f%%\n", stats.SuccessRate*100)
		fmt.Printf("   Confidence: %.1f%%\n\n", stats.ConfidenceScore*100)
	}

	// Display final summary
	fmt.Println("=== FINAL SUMMARY ===\n")
	allStats, err := learningDB.GetAllCityStats()
	if err != nil {
		log.Fatalf("Failed to get stats: %v", err)
	}

	for _, stats := range allStats {
		fmt.Printf("📍 %s (%s)\n", strings.Title(stats.City), stats.Timezone)
		fmt.Printf("   ⭐ OPTIMAL ENTRY: %02d:%02d (Confidence: %.1f%%)\n\n",
			int(stats.OptimalEntryHour), int((stats.OptimalEntryHour-float64(int(stats.OptimalEntryHour)))*60),
			stats.ConfidenceScore*100)
	}

	fmt.Println("✅ Statistics recalculation complete!")
}
