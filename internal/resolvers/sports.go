package resolvers

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/go-resty/resty/v2"
)

// SportsResolver resolves sports betting markets
type SportsResolver struct {
	BaseResolver
	config *config.Config
	client *resty.Client
	cache  sync.Map
}

// ESPNGameResponse represents ESPN API game response
type ESPNGameResponse struct {
	Events []struct {
		Name         string `json:"name"`
		ShortName    string `json:"shortName"`
		Status       struct {
			Type struct {
				State     string `json:"state"`
				Completed bool   `json:"completed"`
			} `json:"type"`
		} `json:"status"`
		Competitions []struct {
			Competitors []struct {
				Team struct {
					DisplayName string `json:"displayName"`
					Abbreviation string `json:"abbreviation"`
				} `json:"team"`
				Score  string `json:"score"`
				Winner bool   `json:"winner"`
			} `json:"competitors"`
		} `json:"competitions"`
	} `json:"events"`
}

// NewSportsResolver creates a new sports resolver
func NewSportsResolver(cfg *config.Config) *SportsResolver {
	client := resty.New()
	client.SetBaseURL(cfg.ESPNBaseURL)
	client.SetTimeout(10 * time.Second)

	resolver := &SportsResolver{
		config: cfg,
		client: client,
	}
	resolver.SetConfidence(0.99) // Very high confidence for sports results

	return resolver
}

// ParseMarketQuestion extracts sports data from question
func (s *SportsResolver) ParseMarketQuestion(question string) (*MarketData, error) {
	question = strings.ToLower(question)

	data := &MarketData{
		Category: "sports",
		Extra:    make(map[string]interface{}),
	}

	// Pattern: "Will [TEAM A] beat [TEAM B]?"
	pattern1 := regexp.MustCompile(`(?i)will ([a-z\s]+) beat ([a-z\s]+)`)
	if matches := pattern1.FindStringSubmatch(question); len(matches) > 2 {
		team1 := strings.TrimSpace(matches[1])
		team2 := strings.TrimSpace(matches[2])
		data.Teams = []string{team1, team2}
		data.Condition = "win"

		// Try to detect sport type from team names
		data.Sport = detectSportType(team1, team2)

		return data, nil
	}

	// Pattern: "Will [TEAM] beat [TEAM] on [DATE]?"
	pattern2 := regexp.MustCompile(`(?i)will ([a-z\s]+) beat ([a-z\s]+) on ([a-z0-9\s,]+)`)
	if matches := pattern2.FindStringSubmatch(question); len(matches) > 3 {
		team1 := strings.TrimSpace(matches[1])
		team2 := strings.TrimSpace(matches[2])
		data.Teams = []string{team1, team2}
		data.Condition = "win"
		data.Sport = detectSportType(team1, team2)

		dateStr := strings.TrimSpace(matches[3])
		if parsedDate, err := parseDate(dateStr); err == nil {
			data.Date = parsedDate
		}

		return data, nil
	}

	// Pattern 2b: "[TEAM] vs [TEAM] - [TEAM] to win" (common betting format)
	vsPattern := regexp.MustCompile(`(?i)([a-z\s]+) vs\.? ([a-z\s]+)(?:\s*-\s*|\s+)([a-z\s]+) to win`)
	if matches := vsPattern.FindStringSubmatch(question); len(matches) > 3 {
		team1 := strings.TrimSpace(matches[1])
		team2 := strings.TrimSpace(matches[2])
		winner := strings.TrimSpace(matches[3])
		data.Teams = []string{team1, team2}
		data.Condition = "match_winner"
		data.Extra["predicted_winner"] = winner
		data.Sport = detectSportType(team1, team2)
		return data, nil
	}

	// Pattern 2c: "Will [TEAM] win against [TEAM]?"
	winAgainstPattern := regexp.MustCompile(`(?i)will ([a-z\s]+) win against ([a-z\s]+)`)
	if matches := winAgainstPattern.FindStringSubmatch(question); len(matches) > 2 {
		team1 := strings.TrimSpace(matches[1])
		team2 := strings.TrimSpace(matches[2])
		data.Teams = []string{team1, team2}
		data.Condition = "win"
		data.Sport = detectSportType(team1, team2)
		return data, nil
	}

	// Pattern: "Will [TEAM] score over [X] points?"
	pattern3 := regexp.MustCompile(`(?i)will ([a-z\s]+) score (?:over|above) ([0-9]+) points`)
	if matches := pattern3.FindStringSubmatch(question); len(matches) > 2 {
		team := strings.TrimSpace(matches[1])
		data.Teams = []string{team}
		data.Condition = "score_over"
		data.Sport = detectSportType(team, "")

		// Parse threshold
		if threshold, err := parseFloat(matches[2]); err == nil {
			data.Threshold = threshold
		}

		return data, nil
	}

	// Check if it's an eSports question (Counter-Strike, etc.) - we can't resolve these yet
	if strings.Contains(question, "counter-strike") ||
	   strings.Contains(question, "first blood") ||
	   strings.Contains(question, "map handicap") ||
	   strings.Contains(question, "bo1") ||
	   strings.Contains(question, "bo3") ||
	   strings.Contains(question, "esports") {
		return nil, fmt.Errorf("esports markets not supported yet")
	}

	// Check if it's actually a political question misclassified as sports
	if strings.Contains(question, "portugal") ||
	   strings.Contains(question, "election") ||
	   strings.Contains(question, "vote") ||
	   strings.Contains(question, "seguro") {
		return nil, fmt.Errorf("political question, not sports")
	}

	return nil, fmt.Errorf("could not parse sports question")
}

// CheckResolution checks sports game result and returns outcome with confidence
func (s *SportsResolver) CheckResolution(market polymarket.Market) (*string, float64, error) {
	// Parse the question
	data, err := s.ParseMarketQuestion(market.Question)
	if err != nil {
		return nil, 0, err
	}

	// Check if resolution time has passed (if specified)
	if !data.Date.IsZero() && time.Now().Before(data.Date) {
		return nil, 0, nil // Not yet resolvable
	}

	// Check cache first
	cacheKey := fmt.Sprintf("%s_%s_%s", data.Sport, strings.Join(data.Teams, "_"), data.Date.Format("2006-01-02"))
	if cached, ok := s.cache.Load(cacheKey); ok {
		result := cached.(CachedResult)
		return &result.Outcome, result.Confidence, nil
	}

	// Determine ESPN sport path
	sportPath := getSportPath(data.Sport)
	if sportPath == "" {
		return nil, 0, fmt.Errorf("unknown sport: %s", data.Sport)
	}

	// Call ESPN API
	var response ESPNGameResponse
	resp, err := s.client.R().
		SetResult(&response).
		SetPathParam("sport", sportPath).
		Get("/{sport}/scoreboard")

	if err != nil {
		return nil, 0, fmt.Errorf("ESPN API error: %w", err)
	}

	if resp.IsError() {
		return nil, 0, fmt.Errorf("ESPN API returned error: %s", resp.Status())
	}

	// Find the relevant game
	var outcome *string
	for _, event := range response.Events {
		// Check if game is completed
		if !event.Status.Type.Completed {
			continue // Game not finished yet
		}

		// Check if teams match
		if len(event.Competitions) == 0 {
			continue
		}

		competition := event.Competitions[0]
		if len(competition.Competitors) < 2 {
			continue
		}

		team1Name := competition.Competitors[0].Team.DisplayName
		team2Name := competition.Competitors[1].Team.DisplayName

		// Check if this is the right game
		teamMatches := false
		if len(data.Teams) >= 2 {
			if (strings.Contains(strings.ToLower(team1Name), strings.ToLower(data.Teams[0])) &&
				strings.Contains(strings.ToLower(team2Name), strings.ToLower(data.Teams[1]))) ||
				(strings.Contains(strings.ToLower(team2Name), strings.ToLower(data.Teams[0])) &&
				strings.Contains(strings.ToLower(team1Name), strings.ToLower(data.Teams[1]))) {
				teamMatches = true
			}
		} else if len(data.Teams) == 1 {
			if strings.Contains(strings.ToLower(team1Name), strings.ToLower(data.Teams[0])) ||
				strings.Contains(strings.ToLower(team2Name), strings.ToLower(data.Teams[0])) {
				teamMatches = true
			}
		}

		if !teamMatches {
			continue
		}

		// Determine outcome based on condition
		switch data.Condition {
		case "win":
			// Check which team won
			var winnerName string
			for _, competitor := range competition.Competitors {
				if competitor.Winner {
					winnerName = competitor.Team.DisplayName
					break
				}
			}

			// Check if the first team in the question won
			if strings.Contains(strings.ToLower(winnerName), strings.ToLower(data.Teams[0])) {
				result := "YES"
				outcome = &result
			} else {
				result := "NO"
				outcome = &result
			}

		case "score_over":
			// Find team score and compare to threshold
			// Implementation would depend on which team is being asked about
			// For now, return nil (not implemented)
			return nil, 0, fmt.Errorf("score_over condition not fully implemented")
		}

		break
	}

	if outcome == nil {
		return nil, 0, nil // Game not found or not finished
	}

	// Sports results are highly reliable once the game is finished (99% confidence)
	confidence := 0.99

	// Cache the result with confidence
	s.cache.Store(cacheKey, CachedResult{
		Outcome:    *outcome,
		Confidence: confidence,
	})

	return outcome, confidence, nil
}

// Helper functions
func detectSportType(team1, team2 string) string {
	combined := strings.ToLower(team1 + " " + team2)

	// NBA teams
	nbaTeams := []string{"lakers", "celtics", "warriors", "heat", "bulls", "knicks", "nets"}
	for _, team := range nbaTeams {
		if strings.Contains(combined, team) {
			return "basketball"
		}
	}

	// NFL teams
	nflTeams := []string{"patriots", "cowboys", "49ers", "packers", "chiefs", "eagles"}
	for _, team := range nflTeams {
		if strings.Contains(combined, team) {
			return "football"
		}
	}

	return "unknown"
}

func getSportPath(sport string) string {
	sportPaths := map[string]string{
		"basketball": "nba",
		"football":   "nfl",
		"baseball":   "mlb",
		"hockey":     "nhl",
		"soccer":     "soccer",
	}

	if path, ok := sportPaths[strings.ToLower(sport)]; ok {
		return path
	}

	return ""
}

func parseFloat(s string) (float64, error) {
	var result float64
	_, err := fmt.Sscanf(s, "%f", &result)
	return result, err
}
