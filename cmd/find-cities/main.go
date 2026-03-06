package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize logger
	utils.Logger = logrus.New()
	utils.Logger.SetLevel(logrus.WarnLevel)

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	client := polymarket.NewClient(cfg)
	markets, err := client.GetActiveMarkets()
	if err != nil {
		log.Fatalf("Failed to fetch markets: %v", err)
	}

	fmt.Println("=== ALL WEATHER MARKETS BY CITY ===\n")

	// Track all weather markets
	cityMarkets := make(map[string][]string)

	// Patterns to extract city names from questions
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)in ([A-Z][a-z]+(?: [A-Z][a-z]+)*)`),
		regexp.MustCompile(`(?i)at ([A-Z][a-z]+(?: [A-Z][a-z]+)*)`),
		regexp.MustCompile(`(?i)([A-Z][a-z]+(?: [A-Z][a-z]+)*) on`),
	}

	for _, market := range markets {
		q := market.Question

		// Check if it's a weather market
		isWeather := strings.Contains(strings.ToLower(q), "temperature") ||
			strings.Contains(strings.ToLower(q), "rain") ||
			strings.Contains(strings.ToLower(q), "precipitation") ||
			strings.Contains(strings.ToLower(q), "wind") ||
			strings.Contains(strings.ToLower(q), "hurricane") ||
			strings.Contains(strings.ToLower(q), "weather") ||
			strings.Contains(strings.ToLower(q), "highest") ||
			strings.Contains(strings.ToLower(q), "high temp")

		if !isWeather {
			continue
		}

		// Try to extract city name
		var city string
		for _, pattern := range patterns {
			matches := pattern.FindStringSubmatch(q)
			if len(matches) > 1 {
				city = matches[1]
				break
			}
		}

		if city == "" {
			// Try lowercase patterns too
			qLower := strings.ToLower(q)
			for _, pattern := range patterns {
				matches := pattern.FindStringSubmatch(qLower)
				if len(matches) > 1 {
					city = matches[1]
					break
				}
			}
		}

		if city != "" {
			cityMarkets[city] = append(cityMarkets[city], q)
		} else {
			cityMarkets["[Unknown]"] = append(cityMarkets["[Unknown]"], q)
		}
	}

	// Sort and print
	type cityCount struct {
		city  string
		count int
	}

	var cities []cityCount
	for city, markets := range cityMarkets {
		cities = append(cities, cityCount{city, len(markets)})
	}

	// Sort by count descending
	for i := 0; i < len(cities); i++ {
		for j := i + 1; j < len(cities); j++ {
			if cities[j].count > cities[i].count {
				cities[i], cities[j] = cities[j], cities[i]
			}
		}
	}

	totalMarkets := 0
	for _, c := range cities {
		fmt.Printf("📍 %s: %d markets\n", c.city, c.count)

		// Show first 3 example questions
		examples := cityMarkets[c.city]
		for i := 0; i < 3 && i < len(examples); i++ {
			fmt.Printf("   - %s\n", examples[i])
		}
		if len(examples) > 3 {
			fmt.Printf("   ... and %d more\n", len(examples)-3)
		}
		fmt.Println()
		totalMarkets += c.count
	}

	fmt.Printf("Total weather markets found: %d\n", totalMarkets)
	fmt.Printf("Total cities: %d\n", len(cities))
}
