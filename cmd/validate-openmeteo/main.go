package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// Validate Open-Meteo accuracy by comparing against IEM (authoritative US source)
// If Open-Meteo matches IEM well for US cities, we can trust it for international cities

type USCity struct {
	Name       string
	IEMStation string
	Lat        float64
	Lon        float64
}

var usCities = []USCity{
	{"Chicago", "KORD", 41.9742, -87.9073},
	{"Seattle", "KSEA", 47.4502, -122.3088},
	{"New York", "KJFK", 40.6413, -73.7781},
	{"Miami", "KMIA", 25.7959, -80.2870},
	{"Dallas", "KDFW", 32.8998, -97.0403},
}

type IEMObservation struct {
	Time time.Time
	Temp float64
}

func main() {
	fmt.Println("=== OPEN-METEO ACCURACY VALIDATION ===")
	fmt.Println("Comparing Open-Meteo vs IEM (authoritative source) for US cities")
	fmt.Println("Goal: Validate Open-Meteo accuracy to determine if trustworthy for international use")
	fmt.Println()

	client := resty.New().SetTimeout(30 * time.Second)

	totalDeviation := 0.0
	totalComparisons := 0
	exactMatches := 0

	for _, city := range usCities {
		fmt.Printf("📍 %s (%s)\n", city.Name, city.IEMStation)

		cityDeviation := 0.0
		cityComparisons := 0
		cityMatches := 0

		// Compare last 30 days
		for daysAgo := 1; daysAgo <= 30; daysAgo++ {
			date := time.Now().AddDate(0, 0, -daysAgo)

			// Fetch IEM high temp (ground truth)
			iemHigh, err := fetchIEMHigh(client, city.IEMStation, date)
			if err != nil {
				continue
			}

			// Fetch Open-Meteo high temp
			meteoHigh, err := fetchOpenMeteoHigh(client, city, date)
			if err != nil {
				continue
			}

			// Convert IEM (Fahrenheit) to Celsius for comparison
			iemHighC := (iemHigh - 32) * 5 / 9
			deviation := math.Abs(meteoHigh - iemHighC)

			cityDeviation += deviation
			cityComparisons++
			totalComparisons++
			totalDeviation += deviation

			// Exact match within 0.5°C tolerance
			if deviation <= 0.5 {
				cityMatches++
				exactMatches++
			}

			// Show first 5 days as examples
			if daysAgo <= 5 {
				fmt.Printf("   %s: IEM=%.1f°C, Open-Meteo=%.1f°C, Δ=%.2f°C %s\n",
					date.Format("Jan 02"),
					iemHighC, meteoHigh, deviation,
					getAccuracyIndicator(deviation))
			}
		}

		if cityComparisons > 0 {
			avgDev := cityDeviation / float64(cityComparisons)
			matchRate := float64(cityMatches) / float64(cityComparisons) * 100

			fmt.Printf("   30-day avg deviation: %.2f°C\n", avgDev)
			fmt.Printf("   Exact matches (±0.5°C): %.1f%% (%d/%d)\n", matchRate, cityMatches, cityComparisons)
			fmt.Println()
		}
	}

	// Overall results
	fmt.Println("=== OVERALL RESULTS ===")
	if totalComparisons > 0 {
		avgDeviation := totalDeviation / float64(totalComparisons)
		matchRate := float64(exactMatches) / float64(totalComparisons) * 100

		fmt.Printf("Total comparisons: %d days across %d cities\n", totalComparisons, len(usCities))
		fmt.Printf("Average deviation: %.2f°C\n", avgDeviation)
		fmt.Printf("Exact match rate: %.1f%% (±0.5°C tolerance)\n\n", matchRate)

		// Verdict
		fmt.Println("=== VERDICT ===")
		if avgDeviation < 0.5 && matchRate > 95 {
			fmt.Println("✅ EXCELLENT: Open-Meteo is highly accurate (< 0.5°C deviation)")
			fmt.Println("   Recommendation: SAFE to use for all international cities")
		} else if avgDeviation < 1.0 && matchRate > 90 {
			fmt.Println("✅ GOOD: Open-Meteo is accurate enough (< 1.0°C deviation)")
			fmt.Println("   Recommendation: SAFE to use, minor discrepancies acceptable")
		} else if avgDeviation < 2.0 && matchRate > 80 {
			fmt.Println("⚠️  ACCEPTABLE: Open-Meteo has some deviation (< 2.0°C)")
			fmt.Println("   Recommendation: USE with caution, verify critical markets")
		} else {
			fmt.Println("❌ UNRELIABLE: Open-Meteo deviates significantly from authoritative source")
			fmt.Println("   Recommendation: DO NOT USE, find alternative data source")
		}
	}
}

func fetchIEMHigh(client *resty.Client, station string, date time.Time) (float64, error) {
	dateStr := date.Format("2006-01-02")

	url := fmt.Sprintf(
		"https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py?station=%s&data=tmpf&year1=%s&month1=%s&day1=%s&year2=%s&month2=%s&day2=%s&tz=Etc/UTC&format=onlycomma&latlon=no&elev=no&missing=null&trace=null&direct=no&report_type=1&report_type=2",
		station,
		dateStr[:4], dateStr[5:7], dateStr[8:10],
		dateStr[:4], dateStr[5:7], dateStr[8:10],
	)

	resp, err := client.R().Get(url)
	if err != nil {
		return 0, err
	}

	// Parse CSV
	observations := parseIEMCSV(resp.String())
	if len(observations) == 0 {
		return 0, fmt.Errorf("no observations")
	}

	// Find highest temperature
	highTemp := -999.0
	for _, obs := range observations {
		if obs.Temp > highTemp {
			highTemp = obs.Temp
		}
	}

	if highTemp == -999 {
		return 0, fmt.Errorf("no valid temperature")
	}

	return highTemp, nil
}

func parseIEMCSV(csv string) []IEMObservation {
	lines := strings.Split(csv, "\n")
	if len(lines) < 2 {
		return nil
	}

	var observations []IEMObservation
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}

		// CSV format: station,valid,tmpf
		tempStr := parts[2]
		if tempStr == "null" || tempStr == "" {
			continue
		}

		var temp float64
		fmt.Sscanf(tempStr, "%f", &temp)

		observations = append(observations, IEMObservation{
			Temp: temp,
		})
	}

	return observations
}

func fetchOpenMeteoHigh(client *resty.Client, city USCity, date time.Time) (float64, error) {
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

func getAccuracyIndicator(deviation float64) string {
	if deviation <= 0.3 {
		return "✅"
	} else if deviation <= 0.7 {
		return "🟡"
	} else {
		return "⚠️"
	}
}
