package parity_arbitrage

import (
	"math"
)

const (
	// PolymarketTakerFee is 3.15% for market orders
	PolymarketTakerFee = 0.0315
	// EstimatedGasFee is a conservative estimate for Polygon gas (~$0.02)
	EstimatedGasFee = 0.02
	// MinSpreadCents is minimum deviation from $1.00 (5 cents)
	MinSpreadCents = 0.05
)

// ProfitabilityCalculator calculates net profit for parity opportunities
type ProfitabilityCalculator struct {
	minProfitThreshold float64
	minSpread          float64
	maxSlippage        float64 // PHASE 8: Maximum allowed slippage
}

// NewProfitabilityCalculator creates a new profitability calculator
func NewProfitabilityCalculator(minProfitThreshold, minSpread float64) *ProfitabilityCalculator {
	return &ProfitabilityCalculator{
		minProfitThreshold: minProfitThreshold,
		minSpread:          minSpread,
		maxSlippage:        0.02, // PHASE 8: 2% default slippage tolerance
	}
}

// NewProfitabilityCalculatorWithSlippage creates a calculator with custom slippage
func NewProfitabilityCalculatorWithSlippage(minProfitThreshold, minSpread, maxSlippage float64) *ProfitabilityCalculator {
	return &ProfitabilityCalculator{
		minProfitThreshold: minProfitThreshold,
		minSpread:          minSpread,
		maxSlippage:        maxSlippage,
	}
}

// CalculateNetProfit calculates the net profit for a parity opportunity
func (pc *ProfitabilityCalculator) CalculateNetProfit(yesPrice, noPrice, positionSize float64) (*ProfitCalculation, error) {
	sum := yesPrice + noPrice
	spread := math.Abs(1.0 - sum)

	// Determine arbitrage type
	isLong := sum < 1.0
	isShort := sum > 1.0

	var calc ProfitCalculation

	if isLong {
		// LONG ARBITRAGE: Buy both sides
		// Cost = (yesPrice + noPrice) * positionSize
		// Payout = 1.00 * positionSize (one side pays out)
		calc.CostToEnter = sum * positionSize
		calc.ExpectedPayout = 1.0 * positionSize
		calc.TradingFees = calc.CostToEnter * PolymarketTakerFee
		calc.GasFees = EstimatedGasFee
		calc.NetProfit = calc.ExpectedPayout - calc.CostToEnter - calc.TradingFees - calc.GasFees

	} else if isShort {
		// SHORT ARBITRAGE: Sell both sides
		// Cost = 1.00 * positionSize (to create split position)
		// Revenue = (yesPrice + noPrice) * positionSize
		calc.CostToEnter = 1.0 * positionSize
		calc.ExpectedPayout = sum * positionSize
		calc.TradingFees = calc.ExpectedPayout * PolymarketTakerFee
		calc.GasFees = EstimatedGasFee
		calc.NetProfit = calc.ExpectedPayout - calc.CostToEnter - calc.TradingFees - calc.GasFees

	} else {
		// No arbitrage opportunity (sum == 1.00)
		calc.Profitable = false
		return &calc, nil
	}

	// Calculate profit percentage
	if calc.CostToEnter > 0 {
		calc.ProfitPercent = calc.NetProfit / calc.CostToEnter
	}

	// Determine if profitable
	calc.Profitable = calc.NetProfit > 0 &&
		calc.ProfitPercent >= pc.minProfitThreshold &&
		spread >= pc.minSpread

	return &calc, nil
}

// IsProfitable checks if an opportunity meets minimum profitability requirements
func (pc *ProfitabilityCalculator) IsProfitable(yesPrice, noPrice float64) bool {
	sum := yesPrice + noPrice
	spread := math.Abs(1.0 - sum)

	// Quick check: spread must be at least minSpread
	if spread < pc.minSpread {
		return false
	}

	// Quick profitability estimate (rough check before full calculation)
	// For LONG: profit ≈ spread - fees
	// For SHORT: profit ≈ spread - fees
	estimatedProfit := spread - (sum * PolymarketTakerFee) - EstimatedGasFee

	return estimatedProfit > 0
}

// CalculatePositionSize determines optimal position size based on profit percentage
func (pc *ProfitabilityCalculator) CalculatePositionSize(yesPrice, noPrice, baseSize float64, netProfitPercent float64) float64 {
	// Scale position by opportunity quality
	// Net profit 3-5%: 50% of base
	// Net profit 5-8%: 75% of base
	// Net profit 8%+: 100% of base

	switch {
	case netProfitPercent >= 0.08:
		return baseSize // 100%
	case netProfitPercent >= 0.05:
		return baseSize * 0.75 // 75%
	case netProfitPercent >= 0.03:
		return baseSize * 0.50 // 50%
	default:
		return 0 // Not profitable enough
	}
}

// GetArbitrageType returns the type of arbitrage (LONG or SHORT)
func GetArbitrageType(yesPrice, noPrice float64) ArbitrageType {
	sum := yesPrice + noPrice
	if sum < 1.0 {
		return LongArbitrage
	}
	return ShortArbitrage
}

// CalculateNetProfitWithSlippage calculates profit accounting for slippage
// PHASE 8: Slippage protection
func (pc *ProfitabilityCalculator) CalculateNetProfitWithSlippage(yesPrice, noPrice, positionSize float64) (*ProfitCalculation, error) {
	// Apply worst-case slippage
	// For LONG: we pay slightly more
	// For SHORT: we receive slightly less

	sum := yesPrice + noPrice
	isLong := sum < 1.0

	adjustedYesPrice := yesPrice
	adjustedNoPrice := noPrice

	if isLong {
		// Worst case: prices increase by maxSlippage when buying
		adjustedYesPrice = yesPrice * (1.0 + pc.maxSlippage)
		adjustedNoPrice = noPrice * (1.0 + pc.maxSlippage)
	} else {
		// Worst case: prices decrease by maxSlippage when selling
		adjustedYesPrice = yesPrice * (1.0 - pc.maxSlippage)
		adjustedNoPrice = noPrice * (1.0 - pc.maxSlippage)
	}

	// Calculate with adjusted prices
	return pc.CalculateNetProfit(adjustedYesPrice, adjustedNoPrice, positionSize)
}

// ShouldAbortOnSlippage checks if actual prices differ too much from expected
func (pc *ProfitabilityCalculator) ShouldAbortOnSlippage(expectedYes, expectedNo, actualYes, actualNo float64) bool {
	yesSlippage := math.Abs(actualYes-expectedYes) / expectedYes
	noSlippage := math.Abs(actualNo-expectedNo) / expectedNo

	if yesSlippage > pc.maxSlippage {
		return true
	}
	if noSlippage > pc.maxSlippage {
		return true
	}

	return false
}
