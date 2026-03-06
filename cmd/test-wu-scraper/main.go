package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/djbro/oracle-weather/pkg/weather"
)

func main() {
	fmt.Println("=== Weather Underground Scraper Test ===")
	fmt.Println()

	// Get ChromeDriver path
	chromeDriverPath := os.Getenv("CHROMEDRIVER_PATH")
	if chromeDriverPath == "" {
		chromeDriverPath = "./bin/chromedriver.exe"
	}

	// Check if ChromeDriver exists
	if _, err := os.Stat(chromeDriverPath); os.IsNotExist(err) {
		fmt.Printf("❌ ChromeDriver not found at: %s\n", chromeDriverPath)
		fmt.Println()
		fmt.Println("Please install ChromeDriver:")
		fmt.Println("1. See SETUP_CHROMEDRIVER.md for instructions")
		fmt.Println("2. Or set CHROMEDRIVER_PATH environment variable")
		fmt.Println()
		fmt.Println("Example:")
		fmt.Println("  export CHROMEDRIVER_PATH=/path/to/chromedriver")
		os.Exit(1)
	}

	fmt.Printf("✅ ChromeDriver found: %s\n", chromeDriverPath)
	fmt.Println()

	// Create WU scraper
	scraper, err := weather.NewWUScraper(chromeDriverPath)
	if err != nil {
		log.Fatalf("Failed to create scraper: %v", err)
	}

	// Start Selenium service
	fmt.Println("🚀 Starting ChromeDriver service...")
	if err := scraper.Start(); err != nil {
		log.Fatalf("Failed to start ChromeDriver: %v", err)
	}
	defer func() {
		fmt.Println("\n🛑 Stopping ChromeDriver service...")
		scraper.Stop()
	}()

	fmt.Println("✅ ChromeDriver service started")
	fmt.Println()

	// Test with London yesterday
	yesterday := time.Now().AddDate(0, 0, -2) // Use 2 days ago for finalized data
	testDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)

	fmt.Printf("📅 Testing: London (EGLL) on %s\n", testDate.Format("2006-01-02"))
	fmt.Println("⏳ Scraping Weather Underground (this takes ~10 seconds)...")
	fmt.Println()

	// Scrape with retry
	data, err := scraper.ScrapeWithRetry("EGLL", testDate, "Europe/London", 3)
	if err != nil {
		log.Fatalf("❌ Scraping failed: %v", err)
	}

	fmt.Println("✅ Scraping successful!")
	fmt.Println()
	fmt.Println("=== Results ===")
	fmt.Printf("Date: %s\n", data.Date.Format("2006-01-02"))
	fmt.Printf("High Temperature: %.1f°F (%.1f°C) at %s\n",
		data.TempHighF, data.TempHighC, data.HighTempTime.Format("15:04 MST"))
	fmt.Printf("Low Temperature: %.1f°F (%.1f°C)\n",
		data.TempLowF, data.TempLowC)
	fmt.Printf("Hourly observations: %d\n", len(data.Observations))
	fmt.Println()

	if len(data.Observations) > 0 {
		fmt.Println("First 5 observations:")
		for i := 0; i < 5 && i < len(data.Observations); i++ {
			obs := data.Observations[i]
			fmt.Printf("  %s: %.1f°F (%.1f°C)\n",
				obs.Time.Format("15:04"),
				obs.TempF,
				obs.TempC)
		}
	}

	fmt.Println()
	fmt.Println("✅ WU Scraper test PASSED!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("1. Compare this temp with Visual Crossing for same date")
	fmt.Println("2. If they match → scraper is working correctly")
	fmt.Println("3. Then integrate into bot for live trading")
}
