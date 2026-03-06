package main

import (
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/go-resty/resty/v2"
)

// City represents a city with its weather station info
type City struct {
	Name          string
	Country       string
	Lat           float64
	Lon           float64
	Timezone      string
	ICAOCode      string // For METAR/SYNOP (e.g., EGLL for London)
	WMOCode       string // For WMO station (e.g., 03772 for London)
	ISDStation    string // For NOAA ISD (e.g., "037720-99999")
	WUStation     string // For Weather Underground
}

// DataSource represents a weather data source
type DataSource struct {
	Name         string
	FetchFunc    func(city City, date time.Time) ([]HourlyTemp, error)
	Description  string
}

// HourlyTemp represents temperature at a specific hour
type HourlyTemp struct {
	Hour        int     // 0-23
	TempC       float64
	Available   bool
	Source      string
}

// DayComparison represents comparison for a single day
type DayComparison struct {
	City        string
	Date        time.Time
	Sources     map[string][]HourlyTemp
	HighTemp    float64
	Consensus   []HourlyTemp // Consensus temps across sources
	Deviations  map[string]float64 // Average deviation per source
}

var cities = []City{
	{
		Name: "London", Country: "UK",
		Lat: 51.5074, Lon: -0.1278,
		Timezone: "Europe/London",
		ICAOCode: "EGLL", WMOCode: "03772",
		ISDStation: "037720-99999",
		WUStation: "EGLL",
	},
	{
		Name: "Paris", Country: "France",
		Lat: 48.8566, Lon: 2.3522,
		Timezone: "Europe/Paris",
		ICAOCode: "LFPB", WMOCode: "07150",
		ISDStation: "071500-99999",
		WUStation: "LFPB",
	},
	{
		Name: "Toronto", Country: "Canada",
		Lat: 43.6532, Lon: -79.3832,
		Timezone: "America/Toronto",
		ICAOCode: "CYYZ", WMOCode: "71508",
		ISDStation: "715080-99999",
		WUStation: "CYYZ",
	},
	{
		Name: "Seoul", Country: "South Korea",
		Lat: 37.5665, Lon: 126.9780,
		Timezone: "Asia/Seoul",
		ICAOCode: "RKSS", WMOCode: "47108",
		ISDStation: "471080-99999",
		WUStation: "RKSS",
	},
	{
		Name: "Buenos Aires", Country: "Argentina",
		Lat: -34.6037, Lon: -58.3816,
		Timezone: "America/Argentina/Buenos_Aires",
		ICAOCode: "SAEZ", WMOCode: "87576",
		ISDStation: "875760-99999",
		WUStation: "SAEZ",
	},
	{
		Name: "Ankara", Country: "Turkey",
		Lat: 39.9334, Lon: 32.8597,
		Timezone: "Europe/Istanbul",
		ICAOCode: "LTAC", WMOCode: "17130",
		ISDStation: "171300-99999",
		WUStation: "LTAC",
	},
	{
		Name: "Sao Paulo", Country: "Brazil",
		Lat: -23.5505, Lon: -46.6333,
		Timezone: "America/Sao_Paulo",
		ICAOCode: "SBGR", WMOCode: "83780",
		ISDStation: "837800-99999",
		WUStation: "SBGR",
	},
	{
		Name: "Wellington", Country: "New Zealand",
		Lat: -41.2865, Lon: 174.7762,
		Timezone: "Pacific/Auckland",
		ICAOCode: "NZWN", WMOCode: "93439",
		ISDStation: "934390-99999",
		WUStation: "NZWN",
	},
}

func main() {
	fmt.Println("=== WEATHER DATA SOURCE ACCURACY COMPARISON ===")
	fmt.Println("Comparing 1 year of hourly temperature data across multiple sources")
	fmt.Println("Goal: Determine most accurate source for each city/region")
	fmt.Println()

	// Initialize data sources
	sources := []DataSource{
		{
			Name:        "Open-Meteo",
			FetchFunc:   fetchOpenMeteo,
			Description: "Free global API, no rate limits",
		},
		{
			Name:        "Ogimet-SYNOP",
			FetchFunc:   fetchOgimet,
			Description: "Global SYNOP network, free",
		},
		// NOAA ISD would require downloading files, skip for now
		// {
		// 	Name:        "NOAA-ISD",
		// 	FetchFunc:   fetchNOAAISD,
		// 	Description: "NOAA Integrated Surface Database",
		// },
	}

	fmt.Printf("Testing %d cities against %d data sources\n", len(cities), len(sources))
	fmt.Printf("Date range: %s to %s\n\n", time.Now().AddDate(-1, 0, 0).Format("2006-01-02"), time.Now().Format("2006-01-02"))

	// Sample test: Compare last 7 days for each city
	// (Full year would take too long for initial test)
	results := make(map[string]map[string]*SourceStats)

	for _, city := range cities {
		fmt.Printf("🌍 Testing: %s, %s\n", city.Name, city.Country)

		results[city.Name] = make(map[string]*SourceStats)
		for _, source := range sources {
			results[city.Name][source.Name] = &SourceStats{
				CityName:   city.Name,
				SourceName: source.Name,
			}
		}

		// Test last 7 days
		for daysAgo := 1; daysAgo <= 7; daysAgo++ {
			date := time.Now().AddDate(0, 0, -daysAgo)

			comparison := DayComparison{
				City:    city.Name,
				Date:    date,
				Sources: make(map[string][]HourlyTemp),
			}

			// Fetch from each source
			for _, source := range sources {
				temps, err := source.FetchFunc(city, date)
				if err != nil {
					fmt.Printf("   ⚠️  %s failed for %s: %v\n", source.Name, date.Format("Jan 02"), err)
					results[city.Name][source.Name].FailedDays++
					continue
				}

				comparison.Sources[source.Name] = temps

				// Calculate high temp for this source
				highTemp := -999.0
				validReadings := 0
				for _, temp := range temps {
					if temp.Available && temp.TempC > highTemp {
						highTemp = temp.TempC
					}
					if temp.Available {
						validReadings++
					}
				}

				if validReadings > 0 {
					results[city.Name][source.Name].SuccessfulDays++
					results[city.Name][source.Name].TotalReadings += validReadings
					results[city.Name][source.Name].HighTemps = append(
						results[city.Name][source.Name].HighTemps, highTemp)
				}
			}

			// Calculate consensus and deviations
			analyzeComparison(&comparison, results[city.Name])
		}

		fmt.Println()
	}

	// Print results
	fmt.Println("\n=== COMPARISON RESULTS ===\n")

	for _, city := range cities {
		fmt.Printf("📍 %s, %s\n", city.Name, city.Country)

		for _, source := range sources {
			stats := results[city.Name][source.Name]
			if stats.SuccessfulDays == 0 {
				fmt.Printf("   ❌ %s: No data\n", source.Name)
				continue
			}

			availability := float64(stats.SuccessfulDays) / 7.0 * 100
			avgReadings := float64(stats.TotalReadings) / float64(stats.SuccessfulDays)

			fmt.Printf("   %s:\n", source.Name)
			fmt.Printf("      Availability: %.1f%% (%d/7 days)\n", availability, stats.SuccessfulDays)
			fmt.Printf("      Avg readings/day: %.1f/24 hours\n", avgReadings)
			if stats.AvgDeviation > 0 {
				fmt.Printf("      Avg deviation: %.2f°C\n", stats.AvgDeviation)
			}
		}
		fmt.Println()
	}

	// Generate recommendations
	fmt.Println("=== RECOMMENDATIONS ===\n")
	for _, city := range cities {
		bestSource := ""
		bestScore := 0.0

		for _, source := range sources {
			stats := results[city.Name][source.Name]
			// Score = availability * (1 - normalized_deviation) * readings_completeness
			score := float64(stats.SuccessfulDays) / 7.0
			if stats.TotalReadings > 0 {
				score *= (float64(stats.TotalReadings) / float64(stats.SuccessfulDays) / 24.0)
			}

			if score > bestScore {
				bestScore = score
				bestSource = source.Name
			}
		}

		fmt.Printf("📍 %s: Use **%s** (score: %.2f)\n", city.Name, bestSource, bestScore)
	}
}

type SourceStats struct {
	CityName       string
	SourceName     string
	SuccessfulDays int
	FailedDays     int
	TotalReadings  int
	HighTemps      []float64
	AvgDeviation   float64
	Deviations     []float64
}

func analyzeComparison(comp *DayComparison, cityResults map[string]*SourceStats) {
	// Calculate consensus high temp (average of all sources)
	var allHighs []float64
	for _, temps := range comp.Sources {
		high := -999.0
		for _, t := range temps {
			if t.Available && t.TempC > high {
				high = t.TempC
			}
		}
		if high > -999 {
			allHighs = append(allHighs, high)
		}
	}

	if len(allHighs) == 0 {
		return
	}

	// Consensus = median of all sources
	sum := 0.0
	for _, h := range allHighs {
		sum += h
	}
	consensus := sum / float64(len(allHighs))

	// Calculate deviation for each source
	for sourceName, temps := range comp.Sources {
		high := -999.0
		for _, t := range temps {
			if t.Available && t.TempC > high {
				high = t.TempC
			}
		}

		if high > -999 {
			deviation := math.Abs(high - consensus)
			cityResults[sourceName].Deviations = append(cityResults[sourceName].Deviations, deviation)

			// Update average deviation
			sum := 0.0
			for _, d := range cityResults[sourceName].Deviations {
				sum += d
			}
			cityResults[sourceName].AvgDeviation = sum / float64(len(cityResults[sourceName].Deviations))
		}
	}
}

// Fetch functions for each source
func fetchOpenMeteo(city City, date time.Time) ([]HourlyTemp, error) {
	client := resty.New().SetTimeout(30 * time.Second)

	dateStr := date.Format("2006-01-02")
	url := fmt.Sprintf(
		"https://archive-api.open-meteo.com/v1/archive?latitude=%.4f&longitude=%.4f&start_date=%s&end_date=%s&hourly=temperature_2m&timezone=%s",
		city.Lat, city.Lon, dateStr, dateStr, "UTC",
	)

	var response struct {
		Hourly struct {
			Time        []string  `json:"time"`
			Temperature []float64 `json:"temperature_2m"`
		} `json:"hourly"`
	}

	resp, err := client.R().SetResult(&response).Get(url)
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: %s", resp.Status())
	}

	// Convert to HourlyTemp
	var temps []HourlyTemp
	for i, timeStr := range response.Hourly.Time {
		t, _ := time.Parse("2006-01-02T15:04", timeStr)
		temp := HourlyTemp{
			Hour:      t.Hour(),
			TempC:     response.Hourly.Temperature[i],
			Available: response.Hourly.Temperature[i] != 0, // Assume 0 means no data
			Source:    "Open-Meteo",
		}
		temps = append(temps, temp)
	}

	return temps, nil
}

func fetchOgimet(city City, date time.Time) ([]HourlyTemp, error) {
	client := resty.New().SetTimeout(30 * time.Second)

	// Ogimet SYNOP URL format
	// https://www.ogimet.com/cgi-bin/gsynres?ind=STATION&ano=YEAR&mes=MONTH&day=DAY&hora=23&ndays=1
	url := fmt.Sprintf(
		"https://www.ogimet.com/cgi-bin/gsynres?ind=%s&ano=%d&mes=%02d&day=%02d&hora=23&ndays=1",
		city.WMOCode, date.Year(), date.Month(), date.Day(),
	)

	resp, err := client.R().Get(url)
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error: %s", resp.Status())
	}

	// Parse HTML response to extract temperature data
	// Ogimet returns HTML table with hourly observations
	temps := parseOgimetHTML(resp.String())

	return temps, nil
}

func parseOgimetHTML(html string) []HourlyTemp {
	var temps []HourlyTemp

	// Parse Ogimet HTML to extract hourly temps
	// Format: Look for temperature values in the HTML table
	// This is a simplified parser - real implementation would be more robust

	// Extract temperature values using regex
	// Ogimet format has temperatures in specific table cells
	re := regexp.MustCompile(`<td[^>]*>(-?\d+\.?\d*)</td>`)
	matches := re.FindAllStringSubmatch(html, -1)

	for i, match := range matches {
		if len(match) > 1 {
			var tempC float64
			fmt.Sscanf(match[1], "%f", &tempC)

			temps = append(temps, HourlyTemp{
				Hour:      i % 24,
				TempC:     tempC,
				Available: true,
				Source:    "Ogimet-SYNOP",
			})
		}
	}

	return temps
}
