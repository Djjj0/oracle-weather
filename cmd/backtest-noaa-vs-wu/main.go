package main

import (
	"fmt"
	"math"
	"time"

	"github.com/go-resty/resty/v2"
)

/*
CRITICAL BACKTEST: NOAA ISD vs Weather Underground

Goal: Verify NOAA ISD aligns with WU (Polymarket's resolver source)

Risk: If NOAA shows 34°C but WU shows 35°C, and market asks "Will temp be 35°C or higher?"
      We resolve differently than Polymarket = LOSS even if we're "correct"

Strategy:
1. Fetch 30+ days of high temps from both NOAA ISD and WU
2. Compare exact values
3. Identify deviations > 0.5°C (critical for market resolution)
4. Calculate agreement rate
5. Flag any dangerous disagreements near common thresholds

Verdict:
- If agreement > 95% and deviations < 0.5°C: SAFE to use NOAA ISD
- If agreement < 95% or deviations > 1.0°C: MUST use Weather Underground
*/

type City struct {
	Name        string
	Country     string
	NOAAStation string
	WUStation   string
	Lat         float64
	Lon         float64
}

type Comparison struct {
	Date          time.Time
	NOAAHighC     float64
	WUHighC       float64
	Deviation     float64
	Critical      bool // Deviation > 1.0°C
	NOAAAvailable bool
	WUAvailable   bool
}

var cities = []City{
	{"London", "UK", "037720-99999", "EGLL", 51.5074, -0.1278},
	{"Paris", "France", "071500-99999", "LFPB", 48.8566, 2.3522},
	{"Toronto", "Canada", "715080-99999", "CYYZ", 43.6532, -79.3832},
	{"Seoul", "South Korea", "471080-99999", "RKSS", 37.5665, 126.9780},
	{"Buenos Aires", "Argentina", "875760-99999", "SAEZ", -34.6037, -58.3816},
	{"Ankara", "Turkey", "171300-99999", "LTAC", 39.9334, 32.8597},
	{"Sao Paulo", "Brazil", "837800-99999", "SBGR", -23.5505, -46.6333},
	{"Wellington", "New Zealand", "934390-99999", "NZWN", -41.2865, 174.7762},
}

func main() {
	fmt.Println("=== CRITICAL BACKTEST: NOAA ISD vs Weather Underground ===")
	fmt.Println("Comparing data sources to ensure alignment with Polymarket resolver")
	fmt.Println()
	fmt.Println("⚠️  RISK: If sources disagree, we could lose money on correct predictions!")
	fmt.Println()

	client := resty.New().SetTimeout(30 * time.Second)

	// Test 30 days per city
	totalComparisons := 0
	totalAgreements := 0
	totalCriticalDisagreements := 0
	totalNOAAAvailable := 0
	totalWUAvailable := 0

	cityResults := make(map[string][]Comparison)

	for _, city := range cities {
		fmt.Printf("📍 %s, %s\n", city.Name, city.Country)
		fmt.Printf("   NOAA Station: %s, WU Station: %s\n", city.NOAAStation, city.WUStation)

		var comparisons []Comparison
		agreements := 0
		criticalCount := 0

		for daysAgo := 1; daysAgo <= 30; daysAgo++ {
			date := time.Now().AddDate(0, 0, -daysAgo)

			comp := Comparison{
				Date: date,
			}

			// Fetch NOAA ISD data
			noaaHigh, err := fetchNOAAISD(client, city, date)
			if err == nil {
				comp.NOAAHighC = noaaHigh
				comp.NOAAAvailable = true
				totalNOAAAvailable++
			}

			// Fetch Weather Underground data
			wuHigh, err := fetchWeatherUnderground(client, city, date)
			if err == nil {
				comp.WUHighC = wuHigh
				comp.WUAvailable = true
				totalWUAvailable++
			}

			// Compare if both available
			if comp.NOAAAvailable && comp.WUAvailable {
				totalComparisons++
				comp.Deviation = math.Abs(comp.NOAAHighC - comp.WUHighC)

				// Agreement threshold: ±0.5°C
				if comp.Deviation <= 0.5 {
					agreements++
					totalAgreements++
				}

				// Critical disagreement: > 1.0°C
				if comp.Deviation > 1.0 {
					comp.Critical = true
					criticalCount++
					totalCriticalDisagreements++
				}

				// Show first 5 days
				if daysAgo <= 5 {
					status := "✅"
					if comp.Critical {
						status = "🚨"
					} else if comp.Deviation > 0.5 {
						status = "⚠️"
					}

					fmt.Printf("   %s: NOAA=%.1f°C, WU=%.1f°C, Δ=%.2f°C %s\n",
						date.Format("Jan 02"), comp.NOAAHighC, comp.WUHighC, comp.Deviation, status)
				}
			}

			comparisons = append(comparisons, comp)
		}

		cityResults[city.Name] = comparisons

		// City summary
		if totalComparisons > 0 {
			agreementRate := float64(agreements) / float64(len(comparisons)) * 100
			fmt.Printf("   \n")
			fmt.Printf("   Agreement rate (±0.5°C): %.1f%% (%d/%d days)\n", agreementRate, agreements, len(comparisons))
			fmt.Printf("   Critical disagreements (>1.0°C): %d days\n", criticalCount)

			if criticalCount > 0 {
				fmt.Printf("   ⚠️  WARNING: %d critical deviations detected!\n", criticalCount)
			}
		} else {
			fmt.Printf("   ❌ Could not compare - missing data\n")
		}
		fmt.Println()
	}

	// Overall results
	fmt.Println("=== OVERALL RESULTS ===")
	fmt.Printf("Total comparisons: %d days across %d cities\n", totalComparisons, len(cities))

	if totalComparisons > 0 {
		overallAgreement := float64(totalAgreements) / float64(totalComparisons) * 100
		fmt.Printf("Overall agreement rate: %.1f%% (±0.5°C tolerance)\n", overallAgreement)
		fmt.Printf("Critical disagreements: %d (>1.0°C deviation)\n", totalCriticalDisagreements)
		fmt.Printf("Critical disagreement rate: %.1f%%\n", float64(totalCriticalDisagreements)/float64(totalComparisons)*100)
		fmt.Println()

		// Data availability
		noaaAvailability := float64(totalNOAAAvailable) / float64(len(cities)*30) * 100
		wuAvailability := float64(totalWUAvailable) / float64(len(cities)*30) * 100
		fmt.Printf("NOAA ISD availability: %.1f%% (%d/%d)\n", noaaAvailability, totalNOAAAvailable, len(cities)*30)
		fmt.Printf("WU availability: %.1f%% (%d/%d)\n", wuAvailability, totalWUAvailable, len(cities)*30)
		fmt.Println()

		// VERDICT
		fmt.Println("=== VERDICT ===")
		if overallAgreement >= 95 && totalCriticalDisagreements == 0 {
			fmt.Println("✅ SAFE TO USE NOAA ISD")
			fmt.Println("   NOAA ISD aligns well with Weather Underground")
			fmt.Println("   Risk of misalignment with Polymarket resolver: LOW")
		} else if overallAgreement >= 90 && float64(totalCriticalDisagreements)/float64(totalComparisons) < 0.05 {
			fmt.Println("🟡 ACCEPTABLE WITH CAUTION")
			fmt.Println("   NOAA ISD mostly aligns with Weather Underground")
			fmt.Println("   Risk of misalignment: MODERATE")
			fmt.Println("   Recommendation: Use NOAA but monitor for discrepancies")
		} else {
			fmt.Println("🚨 DANGEROUS - DO NOT USE NOAA ISD")
			fmt.Println("   NOAA ISD deviates significantly from Weather Underground")
			fmt.Println("   Risk of misalignment with Polymarket resolver: HIGH")
			fmt.Println("   Recommendation: MUST use Weather Underground or same source as Polymarket")
			fmt.Println()
			fmt.Println("   Why this matters:")
			fmt.Println("   - Polymarket uses WU for resolution")
			fmt.Println("   - If we use NOAA and it differs, we resolve wrong")
			fmt.Println("   - Result: LOSSES even when we're 'correct' by our data")
		}
	} else {
		fmt.Println("❌ INSUFFICIENT DATA")
		fmt.Println("   Could not fetch enough data from both sources to compare")
	}
}

func fetchNOAAISD(client *resty.Client, city City, date time.Time) (float64, error) {
	// NOAA ISD data is available via FTP/HTTP
	// Format: https://www.ncei.noaa.gov/data/global-hourly/access/YYYY/STATIONID.csv

	// For this backtest, we'll use a placeholder that simulates NOAA ISD access
	// Real implementation would download and parse the CSV files

	// Note: NOAA ISD requires downloading yearly CSV files (not a simple API)
	// This is a significant implementation effort

	return 0, fmt.Errorf("NOAA ISD fetcher not yet implemented - requires CSV parsing")
}

func fetchWeatherUnderground(client *resty.Client, city City, date time.Time) (float64, error) {
	// Weather Underground historical data
	// URL format: https://www.wunderground.com/history/daily/STATION/date/YYYY/M/D

	// WU requires scraping HTML as they don't have a free API for historical data
	// This is also a significant implementation effort

	return 0, fmt.Errorf("Weather Underground scraper not yet implemented - requires HTML parsing")
}
