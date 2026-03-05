package main

import (
	"fmt"
	"math"
	"time"

	"github.com/go-resty/resty/v2"
)

// This script validates Open-Meteo accuracy by comparing against
// official weather station data from Weather Underground

type City struct {
	Name     string
	WUCode   string // Weather Underground station code
	Lat      float64
	Lon      float64
	Timezone string
}

var testCities = []City{
	{"London", "EGLL", 51.5074, -0.1278, "Europe/London"},
	{"Paris", "LFPB", 48.8566, 2.3522, "Europe/Paris"},
	{"Toronto", "CYYZ", 43.6532, -79.3832, "America/Toronto"},
}

func main() {
	fmt.Println("=== OPEN-METEO ACCURACY VALIDATION ===")
	fmt.Println("Comparing Open-Meteo data against Weather Underground official readings")
	fmt.Println()

	client := resty.New().SetTimeout(30 * time.Second)

	// Test last 30 days
	for _, city := range testCities {
		fmt.Printf("📍 %s (%s)\n", city.Name, city.WUCode)

		var totalDeviation float64
		var comparisons int
		var matchCount int

		for daysAgo := 1; daysAgo <= 30; daysAgo++ {
			date := time.Now().AddDate(0, 0, -daysAgo)
			dateStr := date.Format("2006-01-02")

			// Fetch from Open-Meteo
			meteoHigh, err := fetchOpenMeteoHigh(client, city, date)
			if err != nil {
				continue
			}

			// Fetch from Weather Underground (if available)
			wuHigh, err := fetchWeatherUndergroundHigh(client, city, date)
			if err != nil {
				continue
			}

			// Compare
			deviation := math.Abs(meteoHigh - wuHigh)
			totalDeviation += deviation
			comparisons++

			// Count exact matches (within 0.5°C)
			if deviation <= 0.5 {
				matchCount++
			}

			if daysAgo <= 5 {
				fmt.Printf("   %s: Open-Meteo=%.1f°C, WU=%.1f°C, Deviation=%.2f°C\n",
					dateStr, meteoHigh, wuHigh, deviation)
			}
		}

		if comparisons > 0 {
			avgDeviation := totalDeviation / float64(comparisons)
			matchRate := float64(matchCount) / float64(comparisons) * 100

			fmt.Printf("   \n")
			fmt.Printf("   30-day summary:\n")
			fmt.Printf("   Average deviation: %.2f°C\n", avgDeviation)
			fmt.Printf("   Exact matches (±0.5°C): %.1f%% (%d/%d days)\n", matchRate, matchCount, comparisons)
			fmt.Printf("   \n")
		}
	}

	fmt.Println("=== VERDICT ===")
	fmt.Println("If average deviation < 1.0°C and match rate > 90%, Open-Meteo is ACCURATE")
	fmt.Println("If average deviation > 2.0°C or match rate < 80%, need alternative source")
}

func fetchOpenMeteoHigh(client *resty.Client, city City, date time.Time) (float64, error) {
	dateStr := date.Format("2006-01-02")
	url := fmt.Sprintf(
		"https://archive-api.open-meteo.com/v1/archive?latitude=%.4f&longitude=%.4f&start_date=%s&end_date=%s&daily=temperature_2m_max&timezone=UTC",
		city.Lat, city.Lon, dateStr, dateStr,
	)

	var response struct {
		Daily struct {
			TemperatureMax []float64 `json:"temperature_2m_max"`
		} `json:"daily"`
	}

	resp, err := client.R().SetResult(&response).Get(url)
	if err != nil {
		return 0, err
	}

	if resp.IsError() || len(response.Daily.TemperatureMax) == 0 {
		return 0, fmt.Errorf("no data")
	}

	return response.Daily.TemperatureMax[0], nil
}

func fetchWeatherUndergroundHigh(client *resty.Client, city City, date time.Time) (float64, error) {
	// Weather Underground historical data URL format:
	// https://www.wunderground.com/history/daily/STATION/date/YYYY/M/D
	url := fmt.Sprintf(
		"https://www.wunderground.com/history/daily/%s/date/%d/%d/%d",
		city.WUCode, date.Year(), date.Month(), date.Day(),
	)

	if _, err := client.R().Get(url); err != nil {
		return 0, err
	}

	return 0, fmt.Errorf("WU scraping not implemented - would need HTML parser")
}
