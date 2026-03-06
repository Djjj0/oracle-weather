package polymarket

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

// GetResolverLink fetches the resolver link from a Polymarket event page
func (c *PolymarketClientLixv) GetResolverLink(marketID string) (string, error) {
	// We need the market slug to build the URL
	// For now, we'll extract it from the question or fetch market details first
	market, err := c.GetMarketByID(marketID)
	if err != nil {
		return "", fmt.Errorf("failed to get market: %w", err)
	}

	// Try to build URL from question (simplified approach)
	slug := buildSlugFromQuestion(market.Question)
	url := fmt.Sprintf("https://polymarket.com/event/%s", slug)

	return scrapeResolverFromURL(url)
}

// scrapeResolverFromURL fetches and parses the Polymarket page to find WU link
func scrapeResolverFromURL(url string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	html := string(body)

	// Extract wunderground.com link
	wuRegex := regexp.MustCompile(`https://www\.wunderground\.com/history/daily/[^"'\s<>]+`)
	matches := wuRegex.FindStringSubmatch(html)

	if len(matches) == 0 {
		return "", fmt.Errorf("no wunderground link found on page")
	}

	// Clean up the link (remove trailing punctuation/newlines)
	link := regexp.MustCompile(`[.\n]+$`).ReplaceAllString(matches[0], "")

	return link, nil
}

// ExtractStationCode parses the weather station code from a WU URL
// Example: https://www.wunderground.com/history/daily/us/il/chicago/KORD -> "KORD"
func ExtractStationCode(wuURL string) (string, error) {
	stationRegex := regexp.MustCompile(`/([A-Z]{4})$`)
	matches := stationRegex.FindStringSubmatch(wuURL)

	if len(matches) < 2 {
		return "", fmt.Errorf("could not extract station code from URL: %s", wuURL)
	}

	return matches[1], nil
}

// buildSlugFromQuestion creates a URL slug from the question
// This is a simplified version - might need refinement
func buildSlugFromQuestion(question string) string {
	// For now, return empty - we'll need to handle this properly
	// by either storing slugs or using a better URL building strategy
	return ""
}
