package resolvers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/go-resty/resty/v2"
)

// CachedResult stores outcome and confidence
type CachedResult struct {
	Outcome    string
	Confidence float64
}

// CryptoResolver resolves cryptocurrency price markets
type CryptoResolver struct {
	BaseResolver
	config *config.Config
	client *resty.Client
	cache  sync.Map
}

// CoinGeckoResponse represents CoinGecko API response
type CoinGeckoResponse struct {
	MarketData struct {
		CurrentPrice map[string]float64 `json:"current_price"`
	} `json:"market_data"`
}

// CoinGeckoHistoricalResponse represents historical price data
type CoinGeckoHistoricalResponse struct {
	MarketData struct {
		CurrentPrice map[string]float64 `json:"current_price"`
	} `json:"market_data"`
}

// Coin ID mapping
var coinIDMap = map[string]string{
	"btc":      "bitcoin",
	"bitcoin":  "bitcoin",
	"eth":      "ethereum",
	"ethereum": "ethereum",
	"sol":      "solana",
	"solana":   "solana",
	"ada":      "cardano",
	"cardano":  "cardano",
	"doge":     "dogecoin",
	"dogecoin": "dogecoin",
	"xrp":      "ripple",
	"ripple":   "ripple",
	"bnb":      "binancecoin",
	"binance":  "binancecoin",
	"usdt":     "tether",
	"tether":   "tether",
	"usdc":     "usd-coin",
	"matic":    "matic-network",
	"polygon":  "matic-network",
	"dot":      "polkadot",
	"polkadot": "polkadot",
	"avax":     "avalanche-2",
	"avalanche": "avalanche-2",
	"link":     "chainlink",
	"chainlink": "chainlink",
	"atom":     "cosmos",
	"cosmos":   "cosmos",
	"near":     "near",
	"uni":      "uniswap",
	"uniswap":  "uniswap",
	"ltc":      "litecoin",
	"litecoin": "litecoin",
	"algo":     "algorand",
	"algorand": "algorand",
}

// NewCryptoResolver creates a new crypto resolver
func NewCryptoResolver(cfg *config.Config) *CryptoResolver {
	client := resty.New()
	client.SetBaseURL(cfg.CoinGeckoBaseURL)
	client.SetTimeout(10 * time.Second)

	resolver := &CryptoResolver{
		config: cfg,
		client: client,
	}
	resolver.SetConfidence(0.98) // Very high confidence for crypto prices

	return resolver
}

// ParseMarketQuestion extracts crypto data from question
func (c *CryptoResolver) ParseMarketQuestion(question string) (*MarketData, error) {
	question = strings.ToLower(question)

	data := &MarketData{
		Category: "crypto",
		Extra:    make(map[string]interface{}),
	}

	// Pattern: "Will [COIN] be above $[PRICE] at [TIME]?"
	pattern1 := regexp.MustCompile(`(?i)will ([a-z]+) be (above|below) \$([0-9,]+) (?:at|on|by) ([a-z0-9\s,:]+)`)
	if matches := pattern1.FindStringSubmatch(question); len(matches) > 4 {
		data.Asset = strings.ToLower(matches[1])
		data.Condition = matches[2]
		priceStr := strings.ReplaceAll(matches[3], ",", "")
		if price, err := strconv.ParseFloat(priceStr, 64); err == nil {
			data.Threshold = price
		}
		dateStr := strings.TrimSpace(matches[4])
		if parsedDate, err := parseDate(dateStr); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// Pattern: "Will [COIN] close above $[PRICE] on [DATE]?"
	pattern2 := regexp.MustCompile(`(?i)will ([a-z]+) close (above|below) \$([0-9,]+) on ([a-z0-9\s,:]+)`)
	if matches := pattern2.FindStringSubmatch(question); len(matches) > 4 {
		data.Asset = strings.ToLower(matches[1])
		data.Condition = matches[2]
		priceStr := strings.ReplaceAll(matches[3], ",", "")
		if price, err := strconv.ParseFloat(priceStr, 64); err == nil {
			data.Threshold = price
		}
		dateStr := strings.TrimSpace(matches[4])
		if parsedDate, err := parseDate(dateStr); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// Pattern 3: "[COIN] price above/below $[PRICE]" (simpler format)
	pattern3 := regexp.MustCompile(`(?i)([a-z]+) (?:price )?(above|below|over|under) \$([0-9,]+)`)
	if matches := pattern3.FindStringSubmatch(question); len(matches) > 3 {
		// Only match if it's a known crypto coin
		asset := strings.ToLower(matches[1])
		if _, ok := coinIDMap[asset]; ok {
			data.Asset = asset
			condition := matches[2]
			if condition == "over" || condition == "above" {
				data.Condition = "above"
			} else {
				data.Condition = "below"
			}
			priceStr := strings.ReplaceAll(matches[3], ",", "")
			if price, err := strconv.ParseFloat(priceStr, 64); err == nil {
				data.Threshold = price
			}
			// Try to find date in the rest of the question
			// For now, assume it's asking about "now" or near-term
			data.Date = time.Now()
			return data, nil
		}
	}

	// Pattern 4: "$[PRICE] [COIN]" (reverse order)
	pattern4 := regexp.MustCompile(`(?i)\$([0-9,]+) ([a-z]+)`)
	if matches := pattern4.FindStringSubmatch(question); len(matches) > 2 {
		asset := strings.ToLower(matches[2])
		if _, ok := coinIDMap[asset]; ok {
			data.Asset = asset
			priceStr := strings.ReplaceAll(matches[1], ",", "")
			if price, err := strconv.ParseFloat(priceStr, 64); err == nil {
				data.Threshold = price
			}
			// Determine condition from question context
			if strings.Contains(question, "above") || strings.Contains(question, "over") || strings.Contains(question, "exceed") {
				data.Condition = "above"
			} else if strings.Contains(question, "below") || strings.Contains(question, "under") {
				data.Condition = "below"
			}
			data.Date = time.Now()
			return data, nil
		}
	}

	// Pattern 5: "[COIN] Up or Down - [DATE], [TIME]"
	// Note: These markets are complex - we'd need reference price at start time
	// For now, return error so they're skipped
	pattern5 := regexp.MustCompile(`(?i)([a-z]+) up or down`)
	if matches := pattern5.FindStringSubmatch(question); len(matches) > 1 {
		// We can't resolve these without knowing the reference price
		return nil, fmt.Errorf("up/down markets require reference price (not supported yet)")
	}

	return nil, fmt.Errorf("could not parse crypto question")
}

// CheckResolution checks crypto price and returns outcome with confidence
// Name returns the human-readable name for this resolver.
func (c *CryptoResolver) Name() string { return "CoinGecko Crypto" }

func (c *CryptoResolver) CheckResolution(market polymarket.Market) (*string, float64, error) {
	// Parse the question
	data, err := c.ParseMarketQuestion(market.Question)
	if err != nil {
		// Non-retryable parsing error - skip this market
		return nil, 0, fmt.Errorf("parse error (non-retryable): %w", err)
	}

	// Check if resolution time has passed
	if time.Now().Before(data.Date) {
		return nil, 0, nil // Not yet resolvable
	}

	// Get coin ID
	coinID, ok := coinIDMap[data.Asset]
	if !ok {
		// Non-retryable error - unknown coin
		return nil, 0, fmt.Errorf("unknown cryptocurrency (non-retryable): %s", data.Asset)
	}

	// Check cache first
	cacheKey := fmt.Sprintf("%s_%s_%f", coinID, data.Date.Format("2006-01-02"), data.Threshold)
	if cached, ok := c.cache.Load(cacheKey); ok {
		result := cached.(CachedResult)
		return &result.Outcome, result.Confidence, nil
	}

	// Call CoinGecko API for current price (or historical if date is in past)
	var currentPrice float64

	// If the date is more than 1 day in the past, use historical endpoint
	if time.Since(data.Date) > 24*time.Hour {
		dateStr := data.Date.Format("02-01-2006")
		var response CoinGeckoHistoricalResponse
		resp, err := c.client.R().
			SetResult(&response).
			SetPathParam("id", coinID).
			SetPathParam("date", dateStr).
			Get("/coins/{id}/history?date={date}")

		if err != nil {
			return nil, 0, fmt.Errorf("CoinGecko API error: %w", err)
		}

		if resp.IsError() {
			return nil, 0, fmt.Errorf("CoinGecko API returned error: %s", resp.Status())
		}

		if price, ok := response.MarketData.CurrentPrice["usd"]; ok {
			currentPrice = price
		} else {
			return nil, 0, fmt.Errorf("no USD price data available")
		}
	} else {
		// Use current price endpoint
		var response CoinGeckoResponse
		resp, err := c.client.R().
			SetResult(&response).
			SetPathParam("id", coinID).
			Get("/coins/{id}")

		if err != nil {
			return nil, 0, fmt.Errorf("CoinGecko API error: %w", err)
		}

		if resp.IsError() {
			return nil, 0, fmt.Errorf("CoinGecko API returned error: %s", resp.Status())
		}

		if price, ok := response.MarketData.CurrentPrice["usd"]; ok {
			currentPrice = price
		} else {
			return nil, 0, fmt.Errorf("no USD price data available")
		}
	}

	// Determine outcome
	var outcome string
	switch data.Condition {
	case "above":
		if currentPrice > data.Threshold {
			outcome = "YES"
		} else {
			outcome = "NO"
		}
	case "below":
		if currentPrice < data.Threshold {
			outcome = "YES"
		} else {
			outcome = "NO"
		}
	default:
		return nil, 0, fmt.Errorf("unknown condition: %s", data.Condition)
	}

	// Crypto prices from CoinGecko are highly reliable (98% confidence)
	confidence := 0.98

	// Cache the result with confidence
	c.cache.Store(cacheKey, CachedResult{
		Outcome:    outcome,
		Confidence: confidence,
	})

	return &outcome, confidence, nil
}
