package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	utils.Logger = logrus.New()
	utils.Logger.SetLevel(logrus.WarnLevel)

	cfg, _ := config.LoadConfig()
	client := polymarket.NewClient(cfg)

	fmt.Println("=== CHECKING ANY RESOLVED TEMPERATURE MARKETS ===\n")

	markets, _ := client.GetRecentClosedMarkets(2000)

	count := 0
	for _, market := range markets {
		q := strings.ToLower(market.Question)

		if (strings.Contains(q, "temperature") || strings.Contains(q, "°f") || strings.Contains(q, "°c")) &&
		   market.ResolvedOutcome != nil {

			count++

			fmt.Printf("\n=== MARKET #%d ===\n", count)
			fmt.Printf("Question: %s\n", market.Question)
			fmt.Printf("End date: %s\n", market.ResolutionTimestamp.Format("2006-01-02"))
			fmt.Printf("Updated at (UTC): %s\n", market.UpdatedAt.Format("2006-01-02 15:04:05 MST"))
			fmt.Printf("Resolved to: %s\n", *market.ResolvedOutcome)

			// Extract city from question
			city := ""
			for _, c := range []string{"chicago", "seattle", "atlanta", "miami", "new york", "london", "tokyo"} {
				if strings.Contains(q, c) {
					city = c
					break
				}
			}

			if city != "" {
				tzMap := map[string]string{
					"chicago": "America/Chicago",
					"seattle": "America/Los_Angeles",
					"atlanta": "America/New_York",
					"miami": "America/New_York",
					"new york": "America/New_York",
				}

				if tz, ok := tzMap[city]; ok {
					loc, _ := time.LoadLocation(tz)
					localTime := market.UpdatedAt.In(loc)
					fmt.Printf("Local time (%s): %s\n", city, localTime.Format("15:04:05 MST"))
				}
			}

			if count >= 15 {
				break
			}
		}
	}

	fmt.Printf("\n\nFound %d resolved temperature markets\n", count)
}
