package main

import (
	"encoding/json"
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
	utils.Logger.SetLevel(logrus.WarnLevel)

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	client := polymarket.NewClient(cfg)

	// Get active markets
	markets, err := client.GetActiveMarkets()
	if err != nil {
		log.Fatalf("Failed to fetch markets: %v", err)
	}

	// Find a weather market
	var weatherMarket *polymarket.Market
	for i := range markets {
		q := strings.ToLower(markets[i].Question)
		if strings.Contains(q, "temperature") || strings.Contains(q, "highest") {
			weatherMarket = &markets[i]
			break
		}
	}

	if weatherMarket == nil {
		fmt.Println("No weather markets found")
		return
	}

	fmt.Println("=== WEATHER MARKET FROM GAMMA API (LIST) ===")
	fmt.Printf("Market ID: %s\n", weatherMarket.ID)
	fmt.Printf("Question: %s\n\n", weatherMarket.Question)

	// Marshal to JSON to see all fields
	data, _ := json.MarshalIndent(weatherMarket, "", "  ")
	fmt.Println("Full JSON from /markets list:")
	fmt.Println(string(data))

	fmt.Println("\n\n=== NOW FETCHING DETAILED MARKET BY ID ===")

	// Get detailed market info
	detailedMarket, err := client.GetMarketByID(weatherMarket.ID)
	if err != nil {
		log.Fatalf("Failed to fetch detailed market: %v", err)
	}

	// Marshal detailed market to JSON
	detailedData, _ := json.MarshalIndent(detailedMarket, "", "  ")
	fmt.Println("Full JSON from /markets/{id} endpoint:")
	fmt.Println(string(detailedData))

	// Check for any resolver-related fields
	fmt.Println("\n\n=== CHECKING FOR RESOLVER-RELATED FIELDS ===")

	var rawMap map[string]interface{}
	json.Unmarshal(detailedData, &rawMap)

	fmt.Println("All available fields:")
	for key := range rawMap {
		fmt.Printf("  - %s\n", key)
	}

	// Check description for any links
	fmt.Println("\n\n=== DESCRIPTION CONTENT ===")
	fmt.Println(detailedMarket.Description)
}
