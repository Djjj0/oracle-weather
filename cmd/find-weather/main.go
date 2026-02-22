package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
)

func main() {
	cfg, _ := config.LoadConfig()
	utils.SetupLogger(cfg.LogLevel)
	client := polymarket.NewClient(cfg)

	markets, err := client.GetActiveMarkets()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Scanning %d markets for REAL weather markets...\n\n", len(markets))

	// Better weather patterns - whole word matching
	weatherPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(rain|raining|rainy)\b`),
		regexp.MustCompile(`(?i)\b(temperature|temp|degrees|fahrenheit|celsius)\b`),
		regexp.MustCompile(`(?i)\b(snow|snowing|snowy)\b`),
		regexp.MustCompile(`(?i)\bweather\b`),
		regexp.MustCompile(`(?i)\b(sunny|cloudy|storm|wind)\b`),
		regexp.MustCompile(`(?i)\b(high of|low of)\b`),
	}

	realWeatherMarkets := []polymarket.Market{}

	for _, market := range markets {
		question := market.Question

		// Check if it matches weather patterns
		for _, pattern := range weatherPatterns {
			if pattern.MatchString(question) {
				// But exclude Ukraine/political markets
				if !strings.Contains(strings.ToLower(question), "ukraine") &&
				   !strings.Contains(strings.ToLower(question), "russia") {
					realWeatherMarkets = append(realWeatherMarkets, market)
					break
				}
			}
		}
	}

	fmt.Printf("Found %d REAL weather markets:\n\n", len(realWeatherMarkets))

	for i, market := range realWeatherMarkets {
		if i >= 20 {
			fmt.Printf("... and %d more\n", len(realWeatherMarkets)-20)
			break
		}
		fmt.Printf("[%d] %s\n", i+1, market.Question)
		fmt.Printf("    Resolution: %s | Active: %v\n\n", market.ResolutionTimestamp, market.Active)
	}
}
