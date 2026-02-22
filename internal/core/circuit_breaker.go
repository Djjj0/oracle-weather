package core

import (
	"fmt"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/pkg/utils"
)

// CircuitBreaker prevents catastrophic losses by stopping trading when limits are exceeded
type CircuitBreaker struct {
	mu                sync.RWMutex
	dailyLossLimit    float64
	dailyTradeLimit   int
	currentDailyLoss  float64
	currentDailyTrades int
	lastResetDate     string
	isTripped         bool
	tripReason        string
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(dailyLossLimit float64, dailyTradeLimit int) *CircuitBreaker {
	return &CircuitBreaker{
		dailyLossLimit:  dailyLossLimit,
		dailyTradeLimit: dailyTradeLimit,
		lastResetDate:   time.Now().Format("2006-01-02"),
	}
}

// CheckAndReset resets daily counters if it's a new day
func (cb *CircuitBreaker) CheckAndReset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if today != cb.lastResetDate {
		utils.Logger.Infof("Circuit breaker reset for new day: %s", today)
		cb.currentDailyLoss = 0
		cb.currentDailyTrades = 0
		cb.isTripped = false
		cb.tripReason = ""
		cb.lastResetDate = today
	}
}

// RecordTrade records a trade and checks if limits are exceeded
func (cb *CircuitBreaker) RecordTrade(profit float64) error {
	cb.CheckAndReset()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Record the trade
	cb.currentDailyTrades++

	// Record loss if negative profit
	if profit < 0 {
		cb.currentDailyLoss += -profit // Convert to positive loss
	}

	// Check if trade limit exceeded
	if cb.dailyTradeLimit > 0 && cb.currentDailyTrades >= cb.dailyTradeLimit {
		cb.isTripped = true
		cb.tripReason = fmt.Sprintf("Daily trade limit exceeded (%d/%d trades)",
			cb.currentDailyTrades, cb.dailyTradeLimit)
		utils.Logger.Errorf("🚨 CIRCUIT BREAKER TRIPPED: %s", cb.tripReason)

		// Send alert if Discord webhook configured
		if err := cb.sendAlert(); err != nil {
			utils.Logger.Errorf("Failed to send circuit breaker alert: %v", err)
		}

		return fmt.Errorf("circuit breaker tripped: %s", cb.tripReason)
	}

	// Check if loss limit exceeded
	if cb.dailyLossLimit > 0 && cb.currentDailyLoss >= cb.dailyLossLimit {
		cb.isTripped = true
		cb.tripReason = fmt.Sprintf("Daily loss limit exceeded ($%.2f/$%.2f)",
			cb.currentDailyLoss, cb.dailyLossLimit)
		utils.Logger.Errorf("🚨 CIRCUIT BREAKER TRIPPED: %s", cb.tripReason)

		// Send alert
		if err := cb.sendAlert(); err != nil {
			utils.Logger.Errorf("Failed to send circuit breaker alert: %v", err)
		}

		return fmt.Errorf("circuit breaker tripped: %s", cb.tripReason)
	}

	return nil
}

// CanTrade checks if trading is allowed
func (cb *CircuitBreaker) CanTrade() (bool, string) {
	cb.CheckAndReset()

	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.isTripped {
		return false, cb.tripReason
	}

	return true, ""
}

// GetStatus returns current circuit breaker status
func (cb *CircuitBreaker) GetStatus() CircuitBreakerStatus {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStatus{
		IsTripped:         cb.isTripped,
		TripReason:        cb.tripReason,
		CurrentDailyLoss:  cb.currentDailyLoss,
		DailyLossLimit:    cb.dailyLossLimit,
		CurrentDailyTrades: cb.currentDailyTrades,
		DailyTradeLimit:   cb.dailyTradeLimit,
		LastResetDate:     cb.lastResetDate,
	}
}

// Reset manually resets the circuit breaker (use with caution!)
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	utils.Logger.Warn("Circuit breaker manually reset")
	cb.currentDailyLoss = 0
	cb.currentDailyTrades = 0
	cb.isTripped = false
	cb.tripReason = ""
}

// GetDailyPnL returns current daily P&L (negative = loss)
func (cb *CircuitBreaker) GetDailyPnL() float64 {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Return as negative since losses are stored as positive
	return -cb.currentDailyLoss
}

// GetTradeCount returns current daily trade count
func (cb *CircuitBreaker) GetTradeCount() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return cb.currentDailyTrades
}

// sendAlert sends a critical alert notification
func (cb *CircuitBreaker) sendAlert() error {
	// This would use the Discord notification from utils
	// For now, just log it
	utils.Logger.Errorf("🚨🚨🚨 CIRCUIT BREAKER ALERT 🚨🚨🚨")
	utils.Logger.Errorf("Trading has been STOPPED: %s", cb.tripReason)
	utils.Logger.Errorf("Current daily loss: $%.2f (limit: $%.2f)", cb.currentDailyLoss, cb.dailyLossLimit)
	utils.Logger.Errorf("Current daily trades: %d (limit: %d)", cb.currentDailyTrades, cb.dailyTradeLimit)
	return nil
}

// CircuitBreakerStatus represents the current status
type CircuitBreakerStatus struct {
	IsTripped          bool
	TripReason         string
	CurrentDailyLoss   float64
	DailyLossLimit     float64
	CurrentDailyTrades int
	DailyTradeLimit    int
	LastResetDate      string
}
