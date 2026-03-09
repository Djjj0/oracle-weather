package resolvers

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/djbro/oracle-weather/pkg/weather"
	"github.com/go-resty/resty/v2"
)

// WeatherResolver handles market question parsing and resolver link lookups.
// NOTE: For actual resolution, use IEMWeatherResolver (the active trading resolver).
type WeatherResolver struct {
	BaseResolver
	config        *config.Config
	client        *resty.Client
	resolverCache sync.Map // Cache for resolver links (marketID -> WU URL)
	learningDB    *weather.LearningDB
}

// cityTimezones maps city names to IANA timezone strings
var cityTimezones = map[string]string{
	// US Cities
	"seattle":       "America/Los_Angeles",
	"new york":      "America/New_York",
	"new york city": "America/New_York",
	"los angeles":   "America/Los_Angeles",
	"chicago":       "America/Chicago",
	"houston":       "America/Chicago",
	"phoenix":       "America/Phoenix",
	"philadelphia":  "America/New_York",
	"san antonio":   "America/Chicago",
	"san diego":     "America/Los_Angeles",
	"dallas":        "America/Chicago",
	"miami":         "America/New_York",
	"atlanta":       "America/New_York",
	"boston":        "America/New_York",
	"san francisco": "America/Los_Angeles",
	"denver":        "America/Denver",
	"washington":    "America/New_York",
	"nashville":     "America/Chicago",
	"las vegas":     "America/Los_Angeles",
	"portland":      "America/Los_Angeles",
	"minneapolis":   "America/Chicago",
	"detroit":       "America/New_York",
	"austin":        "America/Chicago",
	"memphis":       "America/Chicago",
	"charlotte":     "America/New_York",
	"tampa":         "America/New_York",
	"orlando":       "America/New_York",
	"kansas city":   "America/Chicago",

	// International Cities
	"buenos aires": "America/Argentina/Buenos_Aires",
	"seoul":        "Asia/Seoul",
	"toronto":      "America/Toronto",
	"sydney":       "Australia/Sydney",
	"melbourne":    "Australia/Melbourne",
	"singapore":    "Asia/Singapore",
	"hong kong":    "Asia/Hong_Kong",
	"beijing":      "Asia/Shanghai",
	"shanghai":     "Asia/Shanghai",
	"dubai":        "Asia/Dubai",
	"mumbai":       "Asia/Kolkata",
	"delhi":        "Asia/Kolkata",
	"moscow":       "Europe/Moscow",
	"berlin":       "Europe/Berlin",
	"madrid":       "Europe/Madrid",
	"rome":         "Europe/Rome",
	"amsterdam":    "Europe/Amsterdam",
	"barcelona":    "Europe/Madrid",
	"vienna":       "Europe/Vienna",
	"prague":       "Europe/Prague",
	"stockholm":    "Europe/Stockholm",
	"copenhagen":   "Europe/Copenhagen",
	"oslo":         "Europe/Oslo",
	"helsinki":     "Europe/Helsinki",
	"brussels":     "Europe/Brussels",
	"lisbon":       "Europe/Lisbon",
	"athens":       "Europe/Athens",
	"warsaw":       "Europe/Warsaw",
	"budapest":     "Europe/Budapest",
	"zurich":       "Europe/Zurich",
	"milan":        "Europe/Rome",
	"munich":       "Europe/Berlin",
	"frankfurt":    "Europe/Berlin",
	"london":       "Europe/London",
	"paris":        "Europe/Paris",
	"tokyo":        "Asia/Tokyo",
	"lucknow":      "Asia/Kolkata",
	"ankara":       "Europe/Istanbul",
	"sao paulo":    "America/Sao_Paulo",
	"wellington":   "Pacific/Auckland",
}

// getTimezone returns the IANA timezone string for a city
func getTimezone(city string) string {
	if tz, ok := cityTimezones[strings.ToLower(city)]; ok {
		return tz
	}
	return "UTC"
}

// NewWeatherResolver creates a new weather resolver
func NewWeatherResolver(cfg *config.Config) *WeatherResolver {
	client := resty.New()
	client.SetTimeout(10 * time.Second)

	learningDB, err := weather.NewLearningDB("./data/learning.db")
	if err != nil {
		utils.Logger.Warnf("Failed to open learning database: %v - will use fallback timing", err)
		learningDB = nil
	}

	resolver := &WeatherResolver{
		config:     cfg,
		client:     client,
		learningDB: learningDB,
	}
	resolver.SetConfidence(0.95)

	return resolver
}

// Name returns the human-readable name for this resolver.
func (w *WeatherResolver) Name() string { return "OpenWeatherMap Weather" }

// ParseMarketQuestion extracts weather data from a market question
func (w *WeatherResolver) ParseMarketQuestion(question string) (*MarketData, error) {
	questionLower := strings.ToLower(question)

	data := &MarketData{
		Category: "weather",
		Extra:    make(map[string]interface{}),
	}

	// CELSIUS PATTERNS
	// Pattern C1: "Will the highest temperature in [city] be [X]°C on [date]?"
	tempCExactPattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be (-?\d+)°?c on ([a-z]+\s+\d+)`)
	if matches := tempCExactPattern.FindStringSubmatch(question); len(matches) > 3 {
		data.Location = strings.TrimSpace(matches[1])
		data.Condition = "temperature_exact"
		tempC, _ := strconv.ParseFloat(matches[2], 64)
		data.Threshold = tempC
		data.Extra["unit"] = "celsius"
		if parsedDate, err := parseDate(strings.TrimSpace(matches[3])); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// Pattern C2: "Will the highest temperature in [city] be [X]°C or higher on [date]?"
	tempCHigherPattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be (-?\d+)°?c or (?:higher|above) on ([a-z]+\s+\d+)`)
	if matches := tempCHigherPattern.FindStringSubmatch(question); len(matches) > 3 {
		data.Location = strings.TrimSpace(matches[1])
		data.Condition = "temperature_above"
		tempC, _ := strconv.ParseFloat(matches[2], 64)
		data.Threshold = tempC
		data.Extra["unit"] = "celsius"
		if parsedDate, err := parseDate(strings.TrimSpace(matches[3])); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// Pattern C3: "Will the highest temperature in [city] be [X]°C or below/lower on [date]?"
	tempCLowerPattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be (-?\d+)°?c or (?:below|lower) on ([a-z]+\s+\d+)`)
	if matches := tempCLowerPattern.FindStringSubmatch(question); len(matches) > 3 {
		data.Location = strings.TrimSpace(matches[1])
		data.Condition = "temperature_below"
		tempC, _ := strconv.ParseFloat(matches[2], 64)
		data.Threshold = tempC
		data.Extra["unit"] = "celsius"
		if parsedDate, err := parseDate(strings.TrimSpace(matches[3])); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// FAHRENHEIT PATTERNS
	// Pattern 1: "Will the highest temperature in [city] be between [X]-[Y]°F on [date]?"
	tempRangePattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be between (\d+)-(\d+)°?f on ([a-z]+\s+\d+)`)
	if matches := tempRangePattern.FindStringSubmatch(question); len(matches) > 4 {
		data.Location = strings.TrimSpace(matches[1])
		data.Condition = "temperature_range"
		low, _ := strconv.ParseFloat(matches[2], 64)
		high, _ := strconv.ParseFloat(matches[3], 64)
		data.Extra["temp_low"] = low
		data.Extra["temp_high"] = high
		if parsedDate, err := parseDate(strings.TrimSpace(matches[4])); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// Pattern 2: "Will the highest temperature in [city] be [X]°F or higher on [date]?"
	tempHigherPattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be (\d+)°?f or higher on ([a-z]+\s+\d+)`)
	if matches := tempHigherPattern.FindStringSubmatch(question); len(matches) > 3 {
		data.Location = strings.TrimSpace(matches[1])
		data.Condition = "temperature_above"
		data.Threshold, _ = strconv.ParseFloat(matches[2], 64)
		if parsedDate, err := parseDate(strings.TrimSpace(matches[3])); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// Pattern 3: "Will the highest temperature in [city] be [X]°F or lower/below on [date]?"
	tempLowerPattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be (\d+)°?f or (?:lower|below) on ([a-z]+\s+\d+)`)
	if matches := tempLowerPattern.FindStringSubmatch(question); len(matches) > 3 {
		data.Location = strings.TrimSpace(matches[1])
		data.Condition = "temperature_below"
		data.Threshold, _ = strconv.ParseFloat(matches[2], 64)
		if parsedDate, err := parseDate(strings.TrimSpace(matches[3])); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// Pattern 4: "Will it rain in [city] on [date]?"
	rainPattern := regexp.MustCompile(`(?i)will it rain in ([a-z\s]+) on ([a-z0-9\s,]+)`)
	if matches := rainPattern.FindStringSubmatch(questionLower); len(matches) > 2 {
		data.Location = strings.TrimSpace(matches[1])
		data.Condition = "rain"
		if parsedDate, err := parseDate(strings.TrimSpace(matches[2])); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// Pattern 4b: Alternative rain patterns
	rainPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)rain in ([a-z\s]+) (?:on|tomorrow|today)`),
		regexp.MustCompile(`(?i)will ([a-z\s]+) (?:get|have) rain`),
		regexp.MustCompile(`(?i)will there be rain in ([a-z\s]+)`),
	}
	for _, pattern := range rainPatterns {
		if matches := pattern.FindStringSubmatch(questionLower); len(matches) > 1 {
			data.Location = strings.TrimSpace(matches[1])
			data.Condition = "rain"
			if strings.Contains(questionLower, "tomorrow") {
				data.Date = time.Now().AddDate(0, 0, 1)
			} else if strings.Contains(questionLower, "today") {
				data.Date = time.Now()
			}
			return data, nil
		}
	}

	// Pattern 5: "Will temperature be above/below [X]°F in [city] on [date]?"
	tempPattern := regexp.MustCompile(`(?i)will (?:the )?temperature be (above|below) (\d+)°?[fc] in ([a-z\s]+) on ([a-z0-9\s,]+)`)
	if matches := tempPattern.FindStringSubmatch(question); len(matches) > 4 {
		data.Condition = "temperature_" + matches[1]
		data.Threshold, _ = strconv.ParseFloat(matches[2], 64)
		data.Location = strings.TrimSpace(matches[3])
		if parsedDate, err := parseDate(strings.TrimSpace(matches[4])); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	return nil, fmt.Errorf("could not parse weather question")
}

// parseDate parses various date string formats
func parseDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(strings.TrimSuffix(dateStr, "?"))

	formats := []string{
		"January 2, 2006",
		"January 2",
		"Jan 2, 2006",
		"Jan 2",
		"2006-01-02",
		"01/02/2006",
		"2 January 2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			year := t.Year()
			if year == 0 {
				year = time.Now().Year()
			}
			// Store as noon UTC so timezone conversions always yield the same calendar date
			t = time.Date(year, t.Month(), t.Day(), 12, 0, 0, 0, time.UTC)
			// If more than 6 months in the future, it's probably last year
			if t.After(time.Now().AddDate(0, 6, 0)) {
				t = t.AddDate(-1, 0, 0)
			}
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse date: %s", dateStr)
}

// getResolverLink fetches the WU resolver link for a market (with caching)
func (w *WeatherResolver) getResolverLink(marketID string, marketQuestion string) (string, error) {
	if cached, ok := w.resolverCache.Load(marketID); ok {
		return cached.(string), nil
	}

	slug := buildSlugFromQuestion(marketQuestion)
	if slug == "" {
		return "", fmt.Errorf("could not build URL slug from question")
	}

	url := fmt.Sprintf("https://polymarket.com/event/%s", slug)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	wuRegex := regexp.MustCompile(`https://www\.wunderground\.com/history/daily/[^"'\s<>]+`)
	matches := wuRegex.FindStringSubmatch(string(body))
	if len(matches) == 0 {
		return "", fmt.Errorf("no wunderground link found on page")
	}

	link := regexp.MustCompile(`[.\n]+$`).ReplaceAllString(matches[0], "")
	w.resolverCache.Store(marketID, link)

	return link, nil
}

// extractStationCode parses the weather station code from a WU URL
// Example: https://www.wunderground.com/history/daily/us/il/chicago/KORD -> "KORD"
func extractStationCode(wuURL string) (string, error) {
	stationRegex := regexp.MustCompile(`/([A-Z]{4})$`)
	matches := stationRegex.FindStringSubmatch(wuURL)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not extract station code from URL: %s", wuURL)
	}
	return matches[1], nil
}

// getOptimalEntryHour looks up optimal entry hour for a weather station
func (w *WeatherResolver) getOptimalEntryHour(stationCode string) (float64, string, error) {
	if w.learningDB == nil {
		return 0, "", fmt.Errorf("learning database not available")
	}
	return w.learningDB.GetOptimalEntryByStation(stationCode)
}

// buildSlugFromQuestion creates a URL slug from a market question
func buildSlugFromQuestion(question string) string {
	slug := strings.ToLower(question)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(slug, "")
	slug = regexp.MustCompile(`-+`).ReplaceAllString(slug, "-")
	return strings.Trim(slug, "-")
}
