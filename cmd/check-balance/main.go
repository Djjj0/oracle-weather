package main

import (
	"fmt"
	"log"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize logger
	utils.Logger = logrus.New()
	utils.Logger.SetLevel(logrus.ErrorLevel) // Only show errors

	// Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize Polymarket client
	fmt.Println("🔍 Checking Polymarket balance...")
	polyClient := polymarket.NewClient(cfg)

	// Get balance
	balance, err := polyClient.GetBalance()
	if err != nil {
		log.Fatalf("❌ Failed to get balance: %v", err)
	}

	// Display balance
	fmt.Println()
	fmt.Println("💰 Your Polymarket Balance")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("USDC: $%.2f\n", balance)
	fmt.Println()

	// Recommend position size
	if balance >= 100 {
		maxTrades := int(balance / 100)
		fmt.Printf("✅ Recommended: $100 per trade (%d max concurrent)\n", maxTrades)
	} else if balance >= 50 {
		maxTrades := int(balance / 50)
		fmt.Printf("✅ Recommended: $50 per trade (%d max concurrent)\n", maxTrades)
	} else if balance >= 25 {
		maxTrades := int(balance / 25)
		fmt.Printf("✅ Recommended: $25 per trade (%d max concurrent)\n", maxTrades)
	} else if balance >= 10 {
		fmt.Println("⚠️  Low balance - Recommended: $10 per trade")
	} else {
		fmt.Println("❌ Insufficient balance for trading")
		fmt.Println("   Minimum: $10 USDC")
	}
}
