package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/djbro/oracle-weather/pkg/weather"
)

func main() {
	fmt.Println("=== Testing Visual Crossing API ===")
	fmt.Println()

	// Get API key from environment
	apiKey := os.Getenv("VISUAL_CROSSING_API_KEY")
	if apiKey == "" {
		log.Fatal("Please set VISUAL_CROSSING_API_KEY environment variable")
	}

	// Create client
	vcClient := weather.NewVCClient(apiKey)
	fmt.Println("✅ Visual Crossing client initialized")
	fmt.Println()

	// Test with London yesterday
	yesterday := time.Now().AddDate(0, 0, -1)
	testDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)

	fmt.Printf("Testing: London on %s\n", testDate.Format("2006-01-02"))
	fmt.Println()

	// Fetch data
	dayData, err := vcClient.FetchDayData("London,UK", testDate)
	if err != nil {
		log.Fatalf("Failed to fetch data: %v", err)
	}

	fmt.Printf("✅ Successfully fetched data\n")
	fmt.Printf("   Date: %s\n", dayData.Datetime)
	fmt.Printf("   Daily High: %.1f°F (%.1f°C)\n", dayData.TempMax, (dayData.TempMax-32)*5/9)
	fmt.Printf("   Daily Low: %.1f°F (%.1f°C)\n", dayData.TempMin, (dayData.TempMin-32)*5/9)
	fmt.Printf("   Hourly observations: %d\n", len(dayData.Hours))
	fmt.Println()

	// Show first 5 and last 5 hourly observations
	fmt.Println("First 5 hourly observations:")
	for i := 0; i < 5 && i < len(dayData.Hours); i++ {
		hour := dayData.Hours[i]
		tempC := (hour.Temp - 32) * 5 / 9
		fmt.Printf("  %s: %.1f°F (%.1f°C) - %s\n",
			hour.Datetime,
			hour.Temp,
			tempC,
			hour.Conditions)
	}

	if len(dayData.Hours) > 10 {
		fmt.Println("  ...")
		fmt.Println("Last 5 hourly observations:")
		for i := len(dayData.Hours) - 5; i < len(dayData.Hours); i++ {
			hour := dayData.Hours[i]
			tempC := (hour.Temp - 32) * 5 / 9
			fmt.Printf("  %s: %.1f°F (%.1f°C) - %s\n",
				hour.Datetime,
				hour.Temp,
				tempC,
				hour.Conditions)
		}
	}

	fmt.Println()

	// Get high temperature and time
	highTempC, highTempTime, err := vcClient.GetHighTemp("London,UK", testDate, "Europe/London")
	if err != nil {
		log.Fatalf("Failed to get high temp: %v", err)
	}

	fmt.Printf("📈 High Temperature: %.1f°C at %s\n",
		highTempC,
		highTempTime.Format("15:04 MST"))

	fmt.Println()

	// Test multiple cities
	fmt.Println("=== Testing International Cities ===")
	cities := []struct {
		name     string
		location string
		timezone string
	}{
		{"Paris", "Paris,France", "Europe/Paris"},
		{"Toronto", "Toronto,Canada", "America/Toronto"},
		{"Seoul", "Seoul,South Korea", "Asia/Seoul"},
	}

	for _, city := range cities {
		highC, highTime, err := vcClient.GetHighTemp(city.location, testDate, city.timezone)
		if err != nil {
			fmt.Printf("❌ %s: %v\n", city.name, err)
			continue
		}
		fmt.Printf("✅ %s: %.1f°C at %s\n", city.name, highC, highTime.Format("15:04 MST"))
	}

	fmt.Println()
	fmt.Println("✅ Visual Crossing API test PASSED!")
	fmt.Println()
	fmt.Println("To get your free API key:")
	fmt.Println("1. Go to: https://www.visualcrossing.com/weather-api")
	fmt.Println("2. Sign up for free account")
	fmt.Println("3. Get API key from dashboard")
	fmt.Println("4. export VISUAL_CROSSING_API_KEY='your_key_here'")
}
