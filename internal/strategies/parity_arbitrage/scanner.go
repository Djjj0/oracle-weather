package parity_arbitrage

import (
	"context"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
)

// OpportunityScanner scans markets for parity arbitrage opportunities
type OpportunityScanner struct {
	client           *polymarket.PolymarketClient
	config           *config.Config
	profitCalc       *ProfitabilityCalculator
	cache            sync.Map // Market price cache
	cacheExpiry      time.Duration
	minLiquidity     float64
	minVolume24h     float64
	maxProbability   float64 // Skip markets where one side >95%
	minMarketAge     time.Duration
	concurrentScans  int
}

// NewOpportunityScanner creates a new opportunity scanner
func NewOpportunityScanner(
	client *polymarket.PolymarketClient,
	config *config.Config,
	profitCalc *ProfitabilityCalculator,
) *OpportunityScanner {
	return &OpportunityScanner{
		client:          client,
		config:          config,
		profitCalc:      profitCalc,
		cacheExpiry:     10 * time.Second,
		minLiquidity:    500.0,  // $500 minimum
		minVolume24h:    100.0,  // $100 minimum
		maxProbability:  0.95,   // 95% max
		minMarketAge:    1 * time.Hour,
		concurrentScans: 100, // Scan 100 markets concurrently
	}
}

// ScanForOpportunities scans all active markets for parity opportunities
func (s *OpportunityScanner) ScanForOpportunities(ctx context.Context) ([]ParityOpportunity, error) {
	utils.Logger.Info("🔍 Starting parity arbitrage scan...")

	// Get all active markets
	markets, err := s.client.GetActiveMarkets()
	if err != nil {
		return nil, err
	}

	utils.Logger.Infof("Scanning %d active markets for parity opportunities", len(markets))

	// Filter markets in parallel
	opportunities := make([]ParityOpportunity, 0)
	oppChan := make(chan ParityOpportunity, 100)
	var wg sync.WaitGroup

	// Create worker pool
	marketsChan := make(chan polymarket.Market, len(markets))
	for i := 0; i < s.concurrentScans; i++ {
		wg.Add(1)
		go s.scanWorker(ctx, marketsChan, oppChan, &wg)
	}

	// Send markets to workers
	go func() {
		for _, market := range markets {
			select {
			case <-ctx.Done():
				close(marketsChan)
				return
			case marketsChan <- market:
			}
		}
		close(marketsChan)
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(oppChan)
	}()

	// Gather opportunities
	for opp := range oppChan {
		opportunities = append(opportunities, opp)
	}

	utils.Logger.Infof("🎯 Found %d parity opportunities", len(opportunities))

	// Sort by profitability (highest first)
	sortByProfitability(opportunities)

	return opportunities, nil
}

// scanWorker scans individual markets for opportunities
func (s *OpportunityScanner) scanWorker(
	ctx context.Context,
	marketsChan <-chan polymarket.Market,
	oppChan chan<- ParityOpportunity,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for market := range marketsChan {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Apply filters
		if !s.shouldScanMarket(market) {
			continue
		}

		// Check for parity opportunity
		opp, found := s.checkParityOpportunity(market)
		if found {
			select {
			case <-ctx.Done():
				return
			case oppChan <- opp:
			}
		}
	}
}

// shouldScanMarket applies filtering criteria
func (s *OpportunityScanner) shouldScanMarket(market polymarket.Market) bool {
	// Must be active
	if market.Closed || !market.Active {
		return false
	}

	// Must have been created at least 1 hour ago
	if time.Since(market.CreatedAt) < s.minMarketAge {
		utils.Logger.Debugf("Skipping %s: too new", market.Question)
		return false
	}

	// Must have minimum liquidity
	if market.Liquidity < s.minLiquidity {
		utils.Logger.Debugf("Skipping %s: low liquidity (%.2f)", market.Question, market.Liquidity)
		return false
	}

	// Must have minimum volume
	if market.Volume < s.minVolume24h {
		utils.Logger.Debugf("Skipping %s: low volume (%.2f)", market.Question, market.Volume)
		return false
	}

	// Must have both YES and NO prices
	if len(market.Prices) < 2 {
		return false
	}

	// Skip markets where one side is >95% (effectively resolved)
	yesPrice := market.Prices[0]
	noPrice := market.Prices[1]
	if yesPrice > s.maxProbability || noPrice > s.maxProbability {
		utils.Logger.Debugf("Skipping %s: one side >95%%", market.Question)
		return false
	}

	// Skip markets near resolution
	if !market.ResolutionTimestamp.IsZero() {
		timeToResolution := time.Until(market.ResolutionTimestamp)
		if timeToResolution < 1*time.Hour && timeToResolution > 0 {
			utils.Logger.Debugf("Skipping %s: near resolution", market.Question)
			return false
		}
	}

	return true
}

// checkParityOpportunity checks if a market has a parity opportunity
func (s *OpportunityScanner) checkParityOpportunity(market polymarket.Market) (ParityOpportunity, bool) {
	if len(market.Prices) < 2 {
		return ParityOpportunity{}, false
	}

	yesPrice := market.Prices[0]
	noPrice := market.Prices[1]

	// Quick profitability check
	if !s.profitCalc.IsProfitable(yesPrice, noPrice) {
		return ParityOpportunity{}, false
	}

	// Full profitability calculation with base position size
	baseSize := s.config.MaxPositionSize
	profitCalc, err := s.profitCalc.CalculateNetProfit(yesPrice, noPrice, baseSize)
	if err != nil || !profitCalc.Profitable {
		return ParityOpportunity{}, false
	}

	// Calculate scaled position size
	positionSize := s.profitCalc.CalculatePositionSize(yesPrice, noPrice, baseSize, profitCalc.ProfitPercent)
	if positionSize <= 0 {
		return ParityOpportunity{}, false
	}

	// Recalculate with actual position size
	finalCalc, _ := s.profitCalc.CalculateNetProfit(yesPrice, noPrice, positionSize)

	// Get token IDs for YES and NO outcomes
	var yesTokenID, noTokenID string
	if len(market.TokenIDs) >= 2 {
		yesTokenID = market.TokenIDs[0]
		noTokenID = market.TokenIDs[1]
	}

	// Create opportunity
	opp := ParityOpportunity{
		MarketID:           market.ID,
		ConditionID:        market.ID, // Use market ID as condition ID
		Question:           market.Question,
		YesTokenID:         yesTokenID,
		NoTokenID:          noTokenID,
		YesPrice:           yesPrice,
		NoPrice:            noPrice,
		PriceSum:           yesPrice + noPrice,
		Type:               GetArbitrageType(yesPrice, noPrice),
		NetProfitPerDollar: finalCalc.ProfitPercent,
		ExpectedProfit:     finalCalc.NetProfit,
		Liquidity:          market.Liquidity,
		Volume24h:          market.Volume,
		DetectedAt:         time.Now(),
		PositionSize:       positionSize,
	}

	utils.Logger.Infof("🎯 Parity opportunity: %s | %s | YES=$%.2f NO=$%.2f Sum=$%.2f | Profit: $%.2f (%.1f%%)",
		market.Question, opp.Type, yesPrice, noPrice, opp.PriceSum, opp.ExpectedProfit, opp.NetProfitPerDollar*100)

	return opp, true
}

// sortByProfitability sorts opportunities by expected profit (highest first)
func sortByProfitability(opportunities []ParityOpportunity) {
	// Simple bubble sort (good enough for small slices)
	n := len(opportunities)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if opportunities[j].ExpectedProfit < opportunities[j+1].ExpectedProfit {
				opportunities[j], opportunities[j+1] = opportunities[j+1], opportunities[j]
			}
		}
	}
}
