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

	fmt.Println("=== CHECKING RECENT RESOLVED CHICAGO MARKETS ===\n")

	markets, _ := client.GetRecentClosedMarkets(2000)

	chicagoCount := 0
	for _, market := range markets {
		q := strings.ToLower(market.Question)

		if strings.Contains(q, "chicago") && 
		   (strings.Contains(q, "temperature") || strings.Contains(q, "°f")) &&
		   market.ResolvedOutcome != nil {

			chicagoCount++

			fmt.Printf("\n=== MARKET #%d ===\n", chicagoCount)
			fmt.Printf("Question: %s\n", market.Question)
			fmt.Printf("Resolution date: %s\n", market.ResolutionTimestamp.Format("2006-01-02"))
			fmt.Printf("Updated at: %s\n", market.UpdatedAt.Format("2006-01-02 15:04:05 MST"))
			fmt.Printf("Resolved to: %s\n", *market.ResolvedOutcome)

			loc, _ := time.LoadLocation("America/Chicago")
			localTime := market.UpdatedAt.In(loc)
			fmt.Printf("Chicago time: %s\n", localTime.Format("15:04:05 MST"))

			if chicagoCount >= 10 {
				break
			}
		}
	}

	fmt.Printf("\n\nFound %d resolved Chicago markets\n", chicagoCount)
}
