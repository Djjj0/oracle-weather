package main

import (
	"fmt"
	"log"
	"time"

	"github.com/djbro/oracle-weather/pkg/weather"
)

func main() {
	fmt.Println("=== Simple WU Scraper Test ===")
	fmt.Println()

	chromeDriverPath := "./bin/chromedriver.exe"

	// Test with London Feb 24
	scraper := weather.NewWUSimpleScraper(chromeDriverPath)

	testDate := time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC)

	fmt.Printf("Scraping WU for: London (EGLL) on %s\n", testDate.Format("2006-01-02"))
	fmt.Println("This takes ~10 seconds...")
	fmt.Println()

	result, err := scraper.GetDailyTemp("EGLL", testDate)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Println("✅ Success!")
	fmt.Println()
	fmt.Printf("High: %.1f°F (%.1f°C)\n", result.HighTempF, result.HighTempC)
	fmt.Printf("Low:  %.1f°F (%.1f°C)\n", result.LowTempF, result.LowTempC)
	fmt.Println()

	// Compare with Visual Crossing
	fmt.Println("=== Comparison ===")
	fmt.Println("Visual Crossing said: 60.4°F (15.8°C)")
	fmt.Printf("Weather Underground:  %.1f°F (%.1f°C)\n", result.HighTempF, result.HighTempC)

	deviation := result.HighTempF - 60.4
	deviationC := result.HighTempC - 15.8

	fmt.Printf("Deviation: %.1f°F (%.1f°C)\n", deviation, deviationC)
	fmt.Println()

	if abs(deviationC) <= 0.5 {
		fmt.Println("✅ Within 0.5°C - SAFE to use VC")
	} else if abs(deviationC) <= 1.0 {
		fmt.Println("🟡 Within 1.0°C - ACCEPTABLE with caution")
	} else {
		fmt.Println("❌ >1.0°C deviation - MUST use WU for live trading")
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
