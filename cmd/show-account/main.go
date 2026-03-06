package main

import (
	"fmt"
	"log"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	// Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Println("🔐 Polymarket Account Details")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Parse private key to get wallet address
	privateKeyHex := cfg.PolymarketPrivateKey
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		log.Fatalf("Invalid private key: %v", err)
	}

	walletAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	// API credentials
	fmt.Println("API Credentials:")
	fmt.Printf("  API Key: %s\n", cfg.PolymarketAPIKey)
	fmt.Printf("  Base URL: %s\n", cfg.PolymarketBaseURL)
	fmt.Println()

	// Wallet details
	fmt.Println("Wallet Details:")
	fmt.Printf("  EOA Address: %s\n", walletAddress.Hex())
	fmt.Printf("  Private Key: %s...%s\n", cfg.PolymarketPrivateKey[:10], cfg.PolymarketPrivateKey[len(cfg.PolymarketPrivateKey)-8:])
	fmt.Println()

	// Magic wallet proxy (hardcoded in client)
	proxyAddress := common.HexToAddress("0x6ff7ae88dbba1834f7647f4153fe30897904931d")
	fmt.Println("Magic Wallet:")
	fmt.Printf("  Proxy Address: %s\n", proxyAddress.Hex())
	fmt.Println()

	// Network
	fmt.Println("Network:")
	fmt.Printf("  Polygon RPC: %s\n", cfg.PolygonRPCURL)
	fmt.Printf("  Chain ID: 137 (Polygon)\n")
	fmt.Println()

	fmt.Println("💡 The balance checker queries the Polymarket API using these credentials")
	fmt.Println("   If balance is wrong, check if this wallet/API key matches your account")
}
