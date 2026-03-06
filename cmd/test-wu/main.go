package main

import (
	"fmt"
	"log"
	"time"

	"github.com/djbro/oracle-weather/pkg/weather"
)

func main() {
	fmt.Println("=== Testing Weather Underground Scraper ===")
	fmt.Println()

	// Initialize WU client
	wuClient := weather.NewWUClient()

	// Test with London yesterday
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		log.Fatalf("Failed to load timezone: %v", err)
	}

	yesterday := time.Now().In(loc).AddDate(0, 0, -1)
	testDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, loc)

	fmt.Printf("Testing: London (EGLL) on %s\n", testDate.Format("2006-01-02"))
	fmt.Println()

	// Fetch data
	observations, err := wuClient.FetchDayData("EGLL", testDate)
	if err != nil {
		log.Fatalf("Failed to fetch data: %v", err)
	}

	fmt.Printf("✅ Successfully fetched %d hourly observations\n\n", len(observations))

	// Show first 5 and last 5 observations
	fmt.Println("First 5 observations:")
	for i := 0; i < 5 && i < len(observations); i++ {
		obs := observations[i]
		fmt.Printf("  %s: %.1f°C (%.1f°F)\n",
			obs.Time.Format("15:04"),
			obs.TempC,
			obs.TempF)
	}

	if len(observations) > 10 {
		fmt.Println("  ...")
		fmt.Println("Last 5 observations:")
		for i := len(observations) - 5; i < len(observations); i++ {
			obs := observations[i]
			fmt.Printf("  %s: %.1f°C (%.1f°F)\n",
				obs.Time.Format("15:04"),
				obs.TempC,
				obs.TempF)
		}
	}

	fmt.Println()

	// Get high temperature
	highTemp, highTempTime, err := wuClient.GetHighTemp("EGLL", testDate)
	if err != nil {
		log.Fatalf("Failed to get high temp: %v", err)
	}

	fmt.Printf("📈 High Temperature: %.1f°C at %s\n",
		highTemp,
		highTempTime.Format("15:04 MST"))

	fmt.Println()
	fmt.Println("✅ Weather Underground scraper test PASSED!")
}
