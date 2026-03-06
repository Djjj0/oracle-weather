package markets

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Market represents a Polymarket market
type Market struct {
	ID              string    `json:"id"`
	Question        string    `json:"question"`
	Description     string    `json:"description"`
	EndDate         time.Time `json:"end_date"`
	Active          bool      `json:"active"`
	Closed          bool      `json:"closed"`
	CurrentPriceYes float64   `json:"current_price_yes"`
	CurrentPriceNo  float64   `json:"current_price_no"`
	Category        string    `json:"category"`
	City            string    // Parsed from question
	Date            time.Time // Parsed from question
	Timezone        string    // Parsed from question
}

// MarketMonitor monitors Polymarket for trading opportunities
type MarketMonitor struct {
	client *http.Client
}

// NewMarketMonitor creates a new market monitor
func NewMarketMonitor() *MarketMonitor {
	return &MarketMonitor{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// GetWeatherMarkets fetches all active weather markets
func (mm *MarketMonitor) GetWeatherMarkets() ([]Market, error) {
	// For now, return hardcoded markets based on our known cities
	// TODO: Integrate with actual Polymarket API

	markets := []Market{
		// US Markets
		{
			ID:       "chicago-temp-today",
			Question: fmt.Sprintf("Will Chicago temperature exceed 32°F on %s?", time.Now().Format("January 2, 2006")),
			Category: "weather",
			City:     "Chicago",
			Timezone: "America/Chicago",
			Date:     time.Now(),
			Active:   true,
		},
		{
			ID:       "seattle-rain-today",
			Question: fmt.Sprintf("Will it rain in Seattle on %s?", time.Now().Format("January 2, 2006")),
			Category: "weather",
			City:     "Seattle",
			Timezone: "America/Los_Angeles",
			Date:     time.Now(),
			Active:   true,
		},
		{
			ID:       "newyork-temp-today",
			Question: fmt.Sprintf("Will New York temperature exceed 40°F on %s?", time.Now().Format("January 2, 2006")),
			Category: "weather",
			City:     "New York",
			Timezone: "America/New_York",
			Date:     time.Now(),
			Active:   true,
		},
		{
			ID:       "miami-temp-today",
			Question: fmt.Sprintf("Will Miami temperature exceed 75°F on %s?", time.Now().Format("January 2, 2006")),
			Category: "weather",
			City:     "Miami",
			Timezone: "America/New_York",
			Date:     time.Now(),
			Active:   true,
		},
		{
			ID:       "dallas-temp-today",
			Question: fmt.Sprintf("Will Dallas temperature exceed 50°F on %s?", time.Now().Format("January 2, 2006")),
			Category: "weather",
			City:     "Dallas",
			Timezone: "America/Chicago",
			Date:     time.Now(),
			Active:   true,
		},
		{
			ID:       "atlanta-temp-today",
			Question: fmt.Sprintf("Will Atlanta temperature exceed 55°F on %s?", time.Now().Format("January 2, 2006")),
			Category: "weather",
			City:     "Atlanta",
			Timezone: "America/New_York",
			Date:     time.Now(),
			Active:   true,
		},
		// International Markets
		{
			ID:       "london-temp-today",
			Question: fmt.Sprintf("Will London temperature exceed 50°F on %s?", time.Now().Format("January 2, 2006")),
			Category: "weather",
			City:     "London",
			Timezone: "Europe/London",
			Date:     time.Now(),
			Active:   true,
		},
		{
			ID:       "paris-temp-today",
			Question: fmt.Sprintf("Will Paris temperature exceed 45°F on %s?", time.Now().Format("January 2, 2006")),
			Category: "weather",
			City:     "Paris",
			Timezone: "Europe/Paris",
			Date:     time.Now(),
			Active:   true,
		},
	}

	return markets, nil
}

// IsReadyForTrading checks if market is past optimal entry time
func (mm *MarketMonitor) IsReadyForTrading(market Market) bool {
	loc, err := time.LoadLocation(market.Timezone)
	if err != nil {
		return false
	}

	now := time.Now().In(loc)
	hour := now.Hour()

	// US markets: trade after 7 PM local
	if isUSCity(market.City) {
		return hour >= 19
	}

	// International markets: trade after 6 PM local (conservative)
	return hour >= 18
}

// ParseMarketQuestion extracts city, date, and threshold from question
func ParseMarketQuestion(question string) (city string, date time.Time, threshold float64, err error) {
	// Simple parsing - can be enhanced with regex
	question = strings.ToLower(question)

	cities := []string{"chicago", "seattle", "new york", "miami", "dallas", "atlanta",
		"london", "paris", "toronto", "seoul", "buenos aires", "ankara", "sao paulo", "wellington"}

	for _, c := range cities {
		if strings.Contains(question, strings.ToLower(c)) {
			city = c
			break
		}
	}

	if city == "" {
		return "", time.Time{}, 0, fmt.Errorf("could not parse city from question")
	}

	// For now, assume today's date
	date = time.Now()

	return city, date, 0, nil
}

func isUSCity(city string) bool {
	usCities := []string{"Chicago", "Seattle", "New York", "Miami", "Dallas", "Atlanta"}
	city = strings.Title(strings.ToLower(city))
	for _, c := range usCities {
		if c == city {
			return true
		}
	}
	return false
}
