package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize logger
	utils.Logger = logrus.New()
	utils.Logger.SetLevel(logrus.InfoLevel)

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	client := polymarket.NewClient(cfg)
	markets, err := client.GetActiveMarkets()
	if err != nil {
		log.Fatalf("Failed to fetch markets: %v", err)
	}

	fmt.Println("=== WEATHER MARKET BREAKDOWN ===\n")

	// Track market types per city
	cityMarkets := make(map[string]map[string]int)
	cities := []string{"seattle", "chicago", "miami", "new york", "phoenix", "los angeles", "denver", "dallas", "houston", "atlanta", "boston", "san francisco"}

	for _, market := range markets {
		q := strings.ToLower(market.Question)

		// Check if it's a weather market
		isWeather := strings.Contains(q, "temperature") ||
			strings.Contains(q, "rain") ||
			strings.Contains(q, "precipitation") ||
			strings.Contains(q, "wind") ||
			strings.Contains(q, "hurricane") ||
			strings.Contains(q, "weather") ||
			strings.Contains(q, "snow") ||
			strings.Contains(q, "storm")

		if !isWeather {
			continue
		}

		// Find which city
		var city string
		for _, c := range cities {
			if strings.Contains(q, c) {
				city = c
				break
			}
		}

		if city == "" {
			continue
		}

		// Determine market type
		var marketType string
		if strings.Contains(q, "highest") || strings.Contains(q, "high temp") {
			marketType = "High Temperature"
		} else if strings.Contains(q, "temperature") && strings.Contains(q, "above") {
			marketType = "Temperature Above X"
		} else if strings.Contains(q, "temperature") && strings.Contains(q, "below") {
			marketType = "Temperature Below X"
		} else if strings.Contains(q, "rain") && strings.Contains(q, "inches") {
			marketType = "Precipitation Amount"
		} else if strings.Contains(q, "rain") {
			marketType = "Rain (Yes/No)"
		} else if strings.Contains(q, "wind") {
			marketType = "Wind Speed"
		} else if strings.Contains(q, "hurricane") {
			marketType = "Hurricane"
		} else if strings.Contains(q, "record") {
			marketType = "Record Temperature"
		} else {
			marketType = "Other Weather"
		}

		if cityMarkets[city] == nil {
			cityMarkets[city] = make(map[string]int)
		}
		cityMarkets[city][marketType]++
	}

	// Print results
	totalMarkets := 0
	for _, city := range cities {
		if len(cityMarkets[city]) == 0 {
			continue
		}

		cityTotal := 0
		for _, count := range cityMarkets[city] {
			cityTotal += count
		}

		fmt.Printf("📍 %s (%d markets)\n", strings.Title(city), cityTotal)
		for marketType, count := range cityMarkets[city] {
			fmt.Printf("   - %s: %d\n", marketType, count)
		}
		fmt.Println()
		totalMarkets += cityTotal
	}

	fmt.Printf("Total weather markets: %d\n", totalMarkets)
}
