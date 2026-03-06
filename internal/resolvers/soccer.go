package resolvers

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/go-resty/resty/v2"
)

// SoccerResolver resolves soccer/football markets
type SoccerResolver struct {
	BaseResolver
	config *config.Config
	client *resty.Client
	cache  sync.Map
}

// APISportsResponse represents API-Football response
type APISportsResponse struct {
	Response []struct {
		Fixture struct {
			ID     int    `json:"id"`
			Status struct {
				Short string `json:"short"` // FT = Full Time, NS = Not Started, etc.
			} `json:"status"`
		} `json:"fixture"`
		Teams struct {
			Home struct {
				Name   string `json:"name"`
				Winner *bool  `json:"winner"`
			} `json:"home"`
			Away struct {
				Name   string `json:"name"`
				Winner *bool  `json:"winner"`
			} `json:"away"`
		} `json:"teams"`
		League struct {
			Name   string `json:"name"`
			Round  string `json:"round"`
		} `json:"league"`
		Goals struct {
			Home *int `json:"home"`
			Away *int `json:"away"`
		} `json:"goals"`
	} `json:"response"`
}

// NewSoccerResolver creates a new soccer resolver
func NewSoccerResolver(cfg *config.Config) *SoccerResolver {
	client := resty.New()
	client.SetTimeout(10 * time.Second)

	resolver := &SoccerResolver{
		config: cfg,
		client: client,
	}
	resolver.SetConfidence(0.99) // Very high confidence for match results

	return resolver
}

// ParseMarketQuestion extracts soccer data from question
func (s *SoccerResolver) ParseMarketQuestion(question string) (*MarketData, error) {
	question = strings.ToLower(question)

	data := &MarketData{
		Category: "soccer",
		Extra:    make(map[string]interface{}),
	}

	// Pattern 1: "Will [TEAM] advance to the [ROUND]?" (Champions League knockout)
	advancePattern := regexp.MustCompile(`(?i)will ([a-z\s.]+?) advance to the ([a-z0-9\s]+)`)
	if matches := advancePattern.FindStringSubmatch(question); len(matches) > 2 {
		team := strings.TrimSpace(matches[1])
		round := strings.TrimSpace(matches[2])

		data.Teams = []string{team}
		data.Condition = "advance"
		data.Extra["round"] = round
		data.Sport = "soccer"

		return data, nil
	}

	// Pattern 2: "Will [TEAM] beat/defeat [TEAM]?"
	beatPattern := regexp.MustCompile(`(?i)will ([a-z\s.]+?) (?:beat|defeat) ([a-z\s.]+)`)
	if matches := beatPattern.FindStringSubmatch(question); len(matches) > 2 {
		team1 := strings.TrimSpace(matches[1])
		team2 := strings.TrimSpace(matches[2])

		data.Teams = []string{team1, team2}
		data.Condition = "win"
		data.Sport = "soccer"

		return data, nil
	}

	// Pattern 3: "Will [TEAM] win the [COMPETITION]?"
	winCompetitionPattern := regexp.MustCompile(`(?i)will ([a-z\s.]+?) win the ([a-z\s]+)`)
	if matches := winCompetitionPattern.FindStringSubmatch(question); len(matches) > 2 {
		team := strings.TrimSpace(matches[1])
		competition := strings.TrimSpace(matches[2])

		data.Teams = []string{team}
		data.Condition = "win_tournament"
		data.Extra["competition"] = competition
		data.Sport = "soccer"

		return data, nil
	}

	// Pattern 4: "[TEAM] vs [TEAM] - [TEAM] to win" (match betting)
	vsPattern := regexp.MustCompile(`(?i)([a-z\s.]+?) vs\.? ([a-z\s.]+?)(?:\s*-\s*|\s+)([a-z\s.]+?) to win`)
	if matches := vsPattern.FindStringSubmatch(question); len(matches) > 3 {
		team1 := strings.TrimSpace(matches[1])
		team2 := strings.TrimSpace(matches[2])
		winner := strings.TrimSpace(matches[3])

		data.Teams = []string{team1, team2}
		data.Condition = "match_winner"
		data.Extra["predicted_winner"] = winner
		data.Sport = "soccer"

		return data, nil
	}

	return nil, fmt.Errorf("could not parse soccer question")
}

// CheckResolution checks soccer match result and returns outcome with confidence
// Name returns the human-readable name for this resolver.
func (s *SoccerResolver) Name() string { return "API-Football Soccer" }

func (s *SoccerResolver) CheckResolution(market polymarket.Market) (*string, float64, error) {
	// Parse the question
	data, err := s.ParseMarketQuestion(market.Question)
	if err != nil {
		return nil, 0, err
	}

	// For "advance to round" markets - we need the actual match result
	// These are hard to resolve without knowing the tournament state
	// For now, return nil (not resolvable with current data)
	if data.Condition == "advance" {
		// Would need to track tournament brackets and aggregate results
		// Example: "Will Real Madrid advance to round of 16?"
		// Requires checking if they won their previous round
		return nil, 0, nil // Not yet resolvable
	}

	// For "win tournament" markets - need to wait for tournament completion
	if data.Condition == "win_tournament" {
		return nil, 0, nil // Not yet resolvable
	}

	// For match winner markets - check if specific match is finished
	if data.Condition == "win" || data.Condition == "match_winner" {
		// Check if resolution time has passed
		if !data.Date.IsZero() && time.Now().Before(data.Date) {
			return nil, 0, nil // Match not yet played
		}

		// Check cache first
		cacheKey := fmt.Sprintf("soccer_%s_%s", strings.Join(data.Teams, "_"), data.Date.Format("2006-01-02"))
		if cached, ok := s.cache.Load(cacheKey); ok {
			result := cached.(CachedResult)
			return &result.Outcome, result.Confidence, nil
		}

		// Try to get match result from API-Football (free tier)
		// NOTE: API-Football free tier is limited - this is a placeholder
		// In production, would need API key and proper integration

		// For now, log that we can't resolve without API
		utils.Logger.Debugf("Soccer match resolution requires API-Football integration: %s",
			strings.Join(data.Teams, " vs "))

		return nil, 0, fmt.Errorf("soccer API integration required (API-Football key needed)")
	}

	return nil, 0, fmt.Errorf("unknown soccer condition: %s", data.Condition)
}

// Note: To fully implement this resolver, you would need:
// 1. API-Football API key (https://www.api-football.com/)
// 2. Add to .env: API_FOOTBALL_KEY=your_key_here
// 3. Implement actual API calls to check match results
//
// For Champions League "advance" markets:
// - Track tournament brackets
// - Aggregate two-leg results
// - Check if team advanced to next round
//
// For now, this resolver will match the markets but return nil (not resolvable)
// until API integration is added.
