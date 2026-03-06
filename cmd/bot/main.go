package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/internal/core"
	"github.com/djbro/oracle-weather/internal/database"
	"github.com/djbro/oracle-weather/internal/resolvers"
	"github.com/djbro/oracle-weather/internal/strategies"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
)

func main() {
	fmt.Println("🌤️  Oracle Weather - Weather Prediction Trading Bot")
	fmt.Println("===================================================")
	fmt.Println()

	// Load configuration
	fmt.Println("Loading configuration...")
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("❌ Failed to load config: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Configuration loaded")

	// Setup logger
	fmt.Println("Setting up logger...")
	logger := utils.SetupLogger(cfg.LogLevel)
	logger.Info("Logger initialized")
	fmt.Println("✅ Logger ready")

	// Initialize database
	fmt.Println("Initializing database...")
	db, err := database.InitDB(cfg.DatabasePath)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	logger.Info("Database initialized")
	fmt.Println("✅ Database ready")

	// Initialize Polymarket client
	fmt.Println("Connecting to Polymarket...")
	client := polymarket.NewClient(cfg)
	logger.Info("Polymarket client initialized")
	fmt.Println("✅ Connected to Polymarket")

	// Check balances and allowances
	fmt.Println("Checking wallet balances...")
	client.CheckBalanceAndAllowance()
	fmt.Println("Ensuring USDC allowances...")
	if err := client.EnsureAllowance(); err != nil {
		logger.Warnf("Allowance check issue: %v", err)
	} else {
		fmt.Println("✅ USDC allowances verified")
	}

	// Initialize monitor and weather resolvers
	fmt.Println("Setting up weather market monitor...")
	monitor := core.NewMonitor(client, cfg)
	factory := resolvers.NewFactory(cfg)
	logger.Info("Monitor and weather resolvers initialized")
	fmt.Println("✅ Weather monitor ready")

	// Initialize Oracle Lag strategy (weather-only)
	fmt.Println("Initializing Weather Oracle Lag strategy...")
	oracleLagStrategy := strategies.NewStrategy(monitor, factory, client, db, cfg)
	logger.Info("Weather Oracle Lag strategy initialized")
	fmt.Println("✅ Weather Oracle Lag strategy ready")


	fmt.Println()
	fmt.Println("============================")
	fmt.Printf("Bot Configuration:\n")
	fmt.Printf("Weather Oracle Lag:\n")
	fmt.Printf("  Min Edge Threshold: %.2f%%\n", cfg.MinProfitThreshold*100)
	fmt.Printf("  Max Position Size: $%.2f\n", cfg.MaxPositionSize)
	fmt.Printf("  Check Interval: %ds\n", cfg.CheckInterval)
	fmt.Printf("\n")
	fmt.Printf("Weather Data Source:\n")
	fmt.Printf("  IEM ASOS (matches Polymarket's Weather Underground)\n")
	fmt.Printf("\n")
	fmt.Printf("Market Filters:\n")
	fmt.Printf("  Min Liquidity: $%.2f\n", cfg.MinLiquidity)
	fmt.Printf("  Min Volume: $%.2f\n", cfg.MinVolume)
	fmt.Printf("  Min Market Age: %dh\n", cfg.MinMarketAge)
	fmt.Printf("  Max Spread: %.2f%%\n", cfg.MaxSpread*100)
	fmt.Printf("\n")
	fmt.Printf("General:\n")
	fmt.Printf("  Log Level: %s\n", cfg.LogLevel)
	fmt.Printf("  Circuit Breaker: $%.0f loss / %d trades\n", cfg.DailyLossLimit, cfg.DailyTradeLimit)
	fmt.Println("============================")
	fmt.Println()

	logger.Info("Starting strategies...")
	fmt.Println("🚀 Bot is running! Press Ctrl+C to stop.")
	fmt.Println()

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Shutdown signal received")
		cancel()
	}()


	// Auto-claim resolved positions every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				result, err := oracleLagStrategy.CheckAndClaimPositions(ctx)
				if err != nil {
					logger.Errorf("Failed to check/claim positions: %v", err)
				} else if result != nil && (result.Wins+result.Losses) > 0 {
					logger.Infof("🏁 Claimed positions — Wins: %d, Losses: %d, Profit: $%.2f",
						result.Wins, result.Losses, result.TotalProfit)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start oracle lag strategy
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.CheckInterval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				logger.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
				logger.Infof("Starting scan at %s", time.Now().Format("2006-01-02 15:04:05"))
				logger.Info("Scanning for weather oracle lag opportunities...")

				// Scan for opportunities
				opportunities, scanResultChan := oracleLagStrategy.ScanOpportunities(ctx)

				// Execute each opportunity
				opportunityCount := 0
				for opp := range opportunities {
					opportunityCount++
					if err := oracleLagStrategy.ExecuteOpportunity(ctx, opp); err != nil {
						logger.Errorf("Failed to execute opportunity: %v", err)
					}
				}

				// Drain and log the scan result summary
				if scanResult, ok := <-scanResultChan; ok {
					logger.Infof("Scan summary: scanned=%d found=%d skipped=%d executed=%d",
						scanResult.MarketsScanned, scanResult.Opportunities, scanResult.Skipped, opportunityCount)
				}

				if opportunityCount == 0 {
					logger.Info("No opportunities found this scan")
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for shutdown
	<-ctx.Done()
	logger.Info("Shutting down gracefully...")
	time.Sleep(2 * time.Second)
	fmt.Println("✅ Shutdown complete")
}
