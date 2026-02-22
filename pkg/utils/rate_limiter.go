package utils

import (
	"sync"
	"time"
)

// RateLimiter implements a sliding window rate limiter
// PHASE 7: Enhanced rate limiting to prevent API bans
type RateLimiter struct {
	limit    int           // Max requests per window
	window   time.Duration // Time window
	requests []time.Time   // Request timestamps
	mu       sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:    limit,
		window:   window,
		requests: make([]time.Time, 0, limit),
	}
}

// Wait blocks until the request can proceed without exceeding rate limit
func (rl *RateLimiter) Wait() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Remove old requests outside the sliding window
	validRequests := rl.requests[:0]
	for _, reqTime := range rl.requests {
		if reqTime.After(cutoff) {
			validRequests = append(validRequests, reqTime)
		}
	}
	rl.requests = validRequests

	// If at limit, wait until oldest request expires
	if len(rl.requests) >= rl.limit {
		oldestRequest := rl.requests[0]
		waitTime := rl.window - now.Sub(oldestRequest)
		if waitTime > 0 {
			Logger.Debugf("Rate limit reached (%d/%d), waiting %s",
				len(rl.requests), rl.limit, waitTime)
			rl.mu.Unlock()
			time.Sleep(waitTime)
			rl.mu.Lock()
		}
	}

	// Record this request
	rl.requests = append(rl.requests, now)
}

// TryWait attempts to proceed without blocking, returns false if would exceed limit
func (rl *RateLimiter) TryWait() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Remove old requests
	validRequests := rl.requests[:0]
	for _, reqTime := range rl.requests {
		if reqTime.After(cutoff) {
			validRequests = append(validRequests, reqTime)
		}
	}
	rl.requests = validRequests

	// Check if we can proceed
	if len(rl.requests) >= rl.limit {
		return false
	}

	// Record this request
	rl.requests = append(rl.requests, now)
	return true
}

// GetCurrentUsage returns current usage (requests in current window)
func (rl *RateLimiter) GetCurrentUsage() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	count := 0
	for _, reqTime := range rl.requests {
		if reqTime.After(cutoff) {
			count++
		}
	}

	return count
}

// Reset clears all recorded requests
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.requests = rl.requests[:0]
	Logger.Debug("Rate limiter reset")
}
