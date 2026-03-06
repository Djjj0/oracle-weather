package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/djbro/oracle-weather/pkg/weather"
)

/*
COMPARISON TEST: Visual Crossing vs Weather Underground

This test compares Visual Crossing API data against manually verified
Weather Underground values to assess accuracy.

Manual verification process:
1. Go to: https://www.wunderground.com/history/daily/EGLL/date/YYYY/M/D
2. Note the daily high and low temperatures shown
3. Compare against Visual Crossing's values

The goal is to determine if VC is accurate enough for our purposes.
Target: < 1°C deviation from WU
*/

type City struct {
	Name      string
	Location  string // For Visual Crossing
	WUStation string // For manual verification
	Timezone  string
}

var testCities = []City{
	{"London", "London,UK", "EGLL", "Europe/London"},
	{"Paris", "Paris,France", "LFPB", "Europe/Paris"},
	{"Toronto", "Toronto,Canada", "CYYZ", "America/Toronto"},
}

func main() {
	fmt.Println("=== Visual Crossing vs Weather Underground Comparison ===")
	fmt.Println()

	// Get Visual Crossing API key
	vcAPIKey := os.Getenv("VISUAL_CROSSING_API_KEY")
	if vcAPIKey == "" {
		fmt.Println("❌ Error: VISUAL_CROSSING_API_KEY not set")
		fmt.Println()
		fmt.Println("Please:")
		fmt.Println("1. Sign up at: https://www.visualcrossing.com/weather-api")
		fmt.Println("2. Get your API key from the dashboard")
		fmt.Println("3. Run: export VISUAL_CROSSING_API_KEY='your_key_here'")
		fmt.Println()
		fmt.Println("See GET_VISUAL_CROSSING_KEY.md for detailed instructions")
		os.Exit(1)
	}

	// Create Visual Crossing client
	vcClient := weather.NewVCClient(vcAPIKey)
	fmt.Println("✅ Visual Crossing client initialized")
	fmt.Println()

	// Test multiple dates (past 7 days for better validation)
	fmt.Println("Testing last 7 days for accuracy assessment...")
	fmt.Println()

	totalComparisons := 0

	for daysAgo := 1; daysAgo <= 7; daysAgo++ {
		yesterday := time.Now().AddDate(0, 0, -daysAgo)
		testDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)

		fmt.Printf("📅 Date: %s (%d days ago)\n", testDate.Format("2006-01-02"), daysAgo)
		fmt.Println(strings.Repeat("=", 70))

		for _, city := range testCities {
			// Fetch Visual Crossing data
			highC, highTime, err := vcClient.GetHighTemp(city.Location, testDate, city.Timezone)
			if err != nil {
				fmt.Printf("❌ %s: Failed to fetch VC data: %v\n", city.Name, err)
				continue
			}

			// Get daily summary
			dailyHighC, dailyLowC, err := vcClient.GetDailyHighLow(city.Location, testDate)
			if err != nil {
				fmt.Printf("❌ %s: Failed to fetch daily summary: %v\n", city.Name, err)
				continue
			}

			// Generate WU verification URL
			wuURL := fmt.Sprintf("https://www.wunderground.com/history/daily/%s/date/%d/%d/%d",
				city.WUStation, testDate.Year(), testDate.Month(), testDate.Day())

			fmt.Printf("\n🌍 %s (%s)\n", city.Name, city.Timezone)
			fmt.Printf("   Visual Crossing Data:\n")
			fmt.Printf("      Daily High: %.1f°C\n", dailyHighC)
			fmt.Printf("      Daily Low:  %.1f°C\n", dailyLowC)
			fmt.Printf("      Hourly Peak: %.1f°C at %s\n", highC, highTime.Format("15:04 MST"))
			fmt.Printf("\n")
			fmt.Printf("   🔍 Manual Verification:\n")
			fmt.Printf("      Go to: %s\n", wuURL)
			fmt.Printf("      Check: Daily High temp shown on WU page\n")
			fmt.Printf("      Compare with VC value above (%.1f°C)\n", dailyHighC)
			fmt.Printf("\n")

			totalComparisons++
		}

		fmt.Println()
	}

	fmt.Println("=== MANUAL COMPARISON INSTRUCTIONS ===")
	fmt.Println()
	fmt.Println("For each city/date above:")
	fmt.Println("1. Open the WU URL in your browser")
	fmt.Println("2. Note the 'High Temperature' shown on the page")
	fmt.Println("3. Compare it to the Visual Crossing value")
	fmt.Println("4. Calculate deviation: |WU_temp - VC_temp|")
	fmt.Println()
	fmt.Println("✅ If deviation < 0.5°C: EXCELLENT accuracy")
	fmt.Println("🟡 If deviation < 1.0°C: ACCEPTABLE accuracy")
	fmt.Println("❌ If deviation > 1.0°C: POOR accuracy (use WU scraping)")
	fmt.Println()
	fmt.Println("Expected result: Visual Crossing should match within 0.5-1°C")
	fmt.Println("(Small deviations are normal due to different data sources)")
	fmt.Println()
	fmt.Println("Total comparisons to verify: ", totalComparisons)
}
