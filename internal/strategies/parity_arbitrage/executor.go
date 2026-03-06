package parity_arbitrage

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
)

const (
	// MaxPriceMovement is maximum allowed price movement before rejecting trade
	MaxPriceMovement = 0.02 // 2%
	// OrderTimeout is maximum time to wait for order fills
	OrderTimeout = 10 * time.Second
)

// TradeExecutor executes parity arbitrage trades
type TradeExecutor struct {
	client *polymarket.PolymarketClient
}

// NewTradeExecutor creates a new trade executor
func NewTradeExecutor(client *polymarket.PolymarketClient) *TradeExecutor {
	return &TradeExecutor{
		client: client,
	}
}

// ExecuteOpportunity executes a parity arbitrage opportunity
func (e *TradeExecutor) ExecuteOpportunity(ctx context.Context, opp ParityOpportunity) (*ExecutionResult, error) {
	utils.Logger.Infof("💰 Executing %s parity arbitrage: %s", opp.Type, opp.Question)
	utils.Logger.Infof("   Position size: $%.2f | Expected profit: $%.2f", opp.PositionSize, opp.ExpectedProfit)

	// Verify prices haven't moved significantly
	if err := e.verifyPrices(opp); err != nil {
		return nil, err
	}

	// Execute based on arbitrage type
	var result *ExecutionResult
	var err error

	switch opp.Type {
	case LongArbitrage:
		result, err = e.executeLongArbitrage(ctx, opp)
	case ShortArbitrage:
		result, err = e.executeShortArbitrage(ctx, opp)
	default:
		return nil, fmt.Errorf("unknown arbitrage type: %s", opp.Type)
	}

	if err != nil {
		return result, err
	}

	// Log result
	if result.Success {
		utils.Logger.Infof("✅ %s arbitrage executed successfully", opp.Type)
		utils.Logger.Infof("   YES filled: %v @ $%.4f | NO filled: %v @ $%.4f",
			result.YesOrderFilled, result.ActualYesPrice,
			result.NoOrderFilled, result.ActualNoPrice)
	} else {
		utils.Logger.Errorf("❌ %s arbitrage failed: %v", opp.Type, result.Error)
		if result.YesOrderFilled != result.NoOrderFilled {
			utils.Logger.Errorf("🚨 PARTIAL FILL DETECTED - Manual intervention required!")
		}
	}

	return result, err
}

// executeLongArbitrage executes a LONG arbitrage (buy both sides)
func (e *TradeExecutor) executeLongArbitrage(ctx context.Context, opp ParityOpportunity) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Opportunity: opp,
		ExecutedAt:  time.Now(),
	}

	// Execute both orders concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, 2)
	priceChan := make(chan struct {
		side  string
		price float64
	}, 2)

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, OrderTimeout)
	defer cancel()

	// Buy YES
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := e.client.PlaceMarketOrder(opp.YesTokenID, opp.YesPrice, opp.PositionSize)
		if err != nil {
			errChan <- fmt.Errorf("YES order failed: %w", err)
			return
		}
		result.YesOrderFilled = true
		result.ActualYesPrice = opp.YesPrice
		priceChan <- struct {
			side  string
			price float64
		}{"YES", opp.YesPrice}
	}()

	// Buy NO
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := e.client.PlaceMarketOrder(opp.NoTokenID, opp.NoPrice, opp.PositionSize)
		if err != nil {
			errChan <- fmt.Errorf("NO order failed: %w", err)
			return
		}
		result.NoOrderFilled = true
		result.ActualNoPrice = opp.NoPrice
		priceChan <- struct {
			side  string
			price float64
		}{"NO", opp.NoPrice}
	}()

	// Wait for completion with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-execCtx.Done():
		result.Success = false
		result.Error = fmt.Errorf("execution timeout")
		return result, result.Error
	case <-done:
		// Check for errors
		close(errChan)
		for err := range errChan {
			if result.Error == nil {
				result.Error = err
			}
		}

		// Both sides filled = success
		result.Success = result.YesOrderFilled && result.NoOrderFilled

		if result.Success {
			// Calculate actual fees paid
			result.FeesPaid = (result.ActualYesPrice + result.ActualNoPrice) * opp.PositionSize * PolymarketTakerFee
		}

		return result, result.Error
	}
}

// executeShortArbitrage executes a SHORT arbitrage (sell both sides)
func (e *TradeExecutor) executeShortArbitrage(ctx context.Context, opp ParityOpportunity) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Opportunity: opp,
		ExecutedAt:  time.Now(),
	}

	utils.Logger.Infof("🔄 Creating split position for SHORT arbitrage...")

	// Step 1: Create split position (lock $1.00 per position → get 1 YES + 1 NO)
	if err := e.client.CreateSplitPosition(opp.MarketID, opp.PositionSize); err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to create split position: %w", err)
		return result, result.Error
	}

	utils.Logger.Infof("✅ Split position created - now have %d YES + %d NO tokens",
		int(opp.PositionSize), int(opp.PositionSize))

	// Step 2: Sell both tokens concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, OrderTimeout)
	defer cancel()

	// Sell YES
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := e.client.PlaceSellOrder(opp.YesTokenID, opp.YesPrice, opp.PositionSize)
		if err != nil {
			errChan <- fmt.Errorf("YES sell failed: %w", err)
			return
		}
		result.YesOrderFilled = true
		result.ActualYesPrice = opp.YesPrice
	}()

	// Sell NO
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := e.client.PlaceSellOrder(opp.NoTokenID, opp.NoPrice, opp.PositionSize)
		if err != nil {
			errChan <- fmt.Errorf("NO sell failed: %w", err)
			return
		}
		result.NoOrderFilled = true
		result.ActualNoPrice = opp.NoPrice
	}()

	// Wait for completion with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-execCtx.Done():
		result.Success = false
		result.Error = fmt.Errorf("execution timeout")

		// If partial fill occurred, we have a problem!
		if result.YesOrderFilled != result.NoOrderFilled {
			utils.Logger.Errorf("🚨 PARTIAL FILL on SHORT - Manual intervention required!")
			// We still have tokens we couldn't sell
		}
		return result, result.Error

	case <-done:
		// Check for errors
		close(errChan)
		for err := range errChan {
			if result.Error == nil {
				result.Error = err
			}
		}

		// Both sides filled = success
		result.Success = result.YesOrderFilled && result.NoOrderFilled

		if result.Success {
			// Calculate actual fees paid
			revenue := (result.ActualYesPrice + result.ActualNoPrice) * opp.PositionSize
			result.FeesPaid = revenue * PolymarketTakerFee
			utils.Logger.Infof("💰 SHORT arbitrage: Revenue $%.2f, Cost $%.2f, Fees $%.2f",
				revenue, opp.PositionSize, result.FeesPaid)
		} else {
			// Partial fill - need to handle leftover tokens
			if result.YesOrderFilled != result.NoOrderFilled {
				utils.Logger.Errorf("🚨 PARTIAL FILL on SHORT arbitrage!")
				utils.Logger.Errorf("   Manual intervention required - leftover tokens in account")
			}
		}

		return result, result.Error
	}
}

// verifyPrices checks that prices haven't moved significantly since detection
func (e *TradeExecutor) verifyPrices(opp ParityOpportunity) error {
	// Re-fetch market data
	market, err := e.client.GetMarketByID(opp.MarketID)
	if err != nil {
		return fmt.Errorf("failed to verify prices: %w", err)
	}

	if len(market.Prices) < 2 {
		return fmt.Errorf("invalid market prices")
	}

	currentYes := market.Prices[0]
	currentNo := market.Prices[1]

	// Check YES price movement
	yesMovement := math.Abs(currentYes-opp.YesPrice) / opp.YesPrice
	if yesMovement > MaxPriceMovement {
		return fmt.Errorf("YES price moved too much: %.2f%% (limit: %.2f%%)",
			yesMovement*100, MaxPriceMovement*100)
	}

	// Check NO price movement
	noMovement := math.Abs(currentNo-opp.NoPrice) / opp.NoPrice
	if noMovement > MaxPriceMovement {
		return fmt.Errorf("NO price moved too much: %.2f%% (limit: %.2f%%)",
			noMovement*100, MaxPriceMovement*100)
	}

	utils.Logger.Debugf("Price verification passed: YES %.4f->%.4f, NO %.4f->%.4f",
		opp.YesPrice, currentYes, opp.NoPrice, currentNo)

	return nil
}

// CheckBalance verifies sufficient balance before execution
func (e *TradeExecutor) CheckBalance(requiredAmount float64) (bool, error) {
	balance, err := e.client.GetBalance()
	if err != nil {
		return false, fmt.Errorf("failed to get balance: %w", err)
	}

	if balance < requiredAmount {
		return false, fmt.Errorf("insufficient balance: have $%.2f, need $%.2f",
			balance, requiredAmount)
	}

	return true, nil
}
