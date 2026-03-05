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
	"github.com/sirupsen/logrus"

)

func main() {
	fmt.Println("🤖 Oracle Weather Trading Bot")
	fmt.Println("============================")
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

	// Initialize monitor and resolvers
	fmt.Println("Setting up market monitor...")
	monitor := core.NewMonitor(client, cfg)
	factory := resolvers.NewFactory(cfg)
	logger.Info("Monitor and resolvers initialized")
	fmt.Println("✅ Monitor ready")

	// Initialize Oracle Lag strategy
	fmt.Println("Initializing trading strategy...")
	strategy := strategies.NewStrategy(monitor, factory, client, db, cfg)
	logger.Info("Oracle Lag strategy initialized")
	fmt.Println("✅ Strategy ready")

	fmt.Println("✅ Position claimer ready")

	fmt.Println()
	fmt.Println("============================")
	fmt.Printf("Bot Configuration:\n")
	fmt.Printf("  Min Profit Threshold: %.2f%%\n", cfg.MinProfitThreshold*100)
	fmt.Printf("  Max Position Size: $%.2f\n", cfg.MaxPositionSize)
	fmt.Printf("  Check Interval: %ds\n", cfg.CheckInterval)
	fmt.Printf("  Position Close Interval: 300s\n")
	fmt.Printf("  Log Level: %s\n", cfg.LogLevel)
	fmt.Println("============================")
	fmt.Println()

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Shutdown signal received, stopping bot...")
		fmt.Println("\n🛑 Shutting down gracefully...")
		cancel()
	}()

	// Start strategy
	logger.Info("Starting strategy...")
	fmt.Println("🚀 Bot is running! Press Ctrl+C to stop.")
	fmt.Println()

	// Run position claimer in background goroutine
	go runPositionClaimer(ctx, strategy, logger)

	runOracleLagStrategy(ctx, strategy, logger, cfg)
}

func runOracleLagStrategy(ctx context.Context, strategy *strategies.OracleLagStrategy, logger *logrus.Logger, cfg *config.Config) {
	ticker := time.NewTicker(time.Duration(cfg.CheckInterval) * time.Second)
	defer ticker.Stop()

	// Run immediately on start
	scanAndExecute(ctx, strategy, logger)

	for {
		select {
		case <-ctx.Done():
			logger.Info("Oracle Lag strategy stopped")
			return
		case <-ticker.C:
			scanAndExecute(ctx, strategy, logger)
		}
	}
}

func runPositionClaimer(ctx context.Context, strategy *strategies.OracleLagStrategy, logger *logrus.Logger) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Run immediately on start
	if result, err := strategy.CheckAndClaimPositions(ctx); err != nil && ctx.Err() == nil {
		logger.Errorf("Position claimer error: %v", err)
	} else if result != nil && (result.Wins > 0 || result.Losses > 0) {
		logger.Infof("Position claim cycle: %d wins, %d losses, P&L: $%.2f", result.Wins, result.Losses, result.TotalProfit)
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("Position claimer stopped")
			return
		case <-ticker.C:
			if result, err := strategy.CheckAndClaimPositions(ctx); err != nil && ctx.Err() == nil {
				logger.Errorf("Position claimer error: %v", err)
			} else if result != nil && (result.Wins > 0 || result.Losses > 0) {
				logger.Infof("Position claim cycle: %d wins, %d losses, P&L: $%.2f", result.Wins, result.Losses, result.TotalProfit)
			}
		}
	}
}

func scanAndExecute(ctx context.Context, strategy *strategies.OracleLagStrategy, logger *logrus.Logger) {
	logger.Info("Starting scan for opportunities...")

	opportunities := strategy.ScanOpportunities(ctx)
	count := 0

	for opp := range opportunities {
		count++
		logger.Infof("Found opportunity #%d", count)

		// Execute opportunity
		if err := strategy.ExecuteOpportunity(ctx, opp); err != nil {
			logger.Errorf("Failed to execute opportunity: %v", err)
		}
	}

	if count == 0 {
		logger.Info("No opportunities found this scan")
	} else {
		logger.Infof("Scan complete - found %d opportunities", count)
	}
}
