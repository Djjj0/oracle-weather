package resolver

import (
	"fmt"
	"strings"
	"time"

	"github.com/djbro/oracle-weather/pkg/weather"
	"github.com/go-resty/resty/v2"
)

// ResolutionResult contains the outcome and confidence
type ResolutionResult struct {
	Outcome    string  // "YES" or "NO"
	Confidence float64 // 0.0 to 1.0
	TempF      float64
	Source     string // "IEM" or "WU"
	Timestamp  time.Time
}

// ResolverMarket is a simplified market for resolution checking
type ResolverMarket struct {
	Question string
	City     string
	Timezone string
	Date     time.Time
}

// Resolver checks if a market can be resolved
type Resolver struct {
	iemClient  *resty.Client
	wuScraper  *weather.WUSimpleScraper
	vcClient   *weather.VCClient
	usCities   map[string]string // city -> IEM station
	intlCities map[string]string // city -> WU station
}

// NewResolver creates a new resolver
func NewResolver(chromeDriverPath, vcAPIKey string) *Resolver {
	return &Resolver{
		iemClient:  resty.New().SetTimeout(30 * time.Second),
		wuScraper:  weather.NewWUSimpleScraper(chromeDriverPath),
		vcClient:   weather.NewVCClient(vcAPIKey),
		usCities: map[string]string{
			"chicago":  "ORD",
			"seattle":  "SEA",
			"new york": "JFK",
			"miami":    "MIA",
			"dallas":   "DFW",
			"atlanta":  "ATL",
		},
		intlCities: map[string]string{
			"london":        "EGLL",
			"paris":         "LFPB",
			"toronto":       "CYYZ",
			"seoul":         "RKSS",
			"buenos aires":  "SAEZ",
			"ankara":        "LTAC",
			"sao paulo":     "SBGR",
			"wellington":    "NZWN",
		},
	}
}

// CheckResolution checks if market can be resolved
func (r *Resolver) CheckResolution(market ResolverMarket) (*ResolutionResult, error) {
	city := strings.ToLower(market.City)

	// Check if US city (use IEM)
	if station, ok := r.usCities[city]; ok {
		return r.checkUSMarket(station, market)
	}

	// Check if international city (use WU scraper)
	if station, ok := r.intlCities[city]; ok {
		return r.checkInternationalMarket(station, market)
	}

	return nil, fmt.Errorf("unknown city: %s", city)
}

func (r *Resolver) checkUSMarket(station string, market ResolverMarket) (*ResolutionResult, error) {
	// Fetch IEM data for today
	dateStr := market.Date.Format("2006-01-02")

	url := fmt.Sprintf("https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py?station=%s&data=tmpf&year1=%s&month1=%s&day1=%s&year2=%s&month2=%s&day2=%s&tz=Etc/UTC&format=onlycomma&latlon=no&elev=no&missing=null&trace=null&direct=no&report_type=1&report_type=2",
		station,
		dateStr[:4], dateStr[5:7], dateStr[8:10],
		dateStr[:4], dateStr[5:7], dateStr[8:10])

	resp, err := r.iemClient.R().Get(url)
	if err != nil {
		return nil, fmt.Errorf("IEM fetch error: %w", err)
	}

	// Parse CSV response
	observations := parseIEMData(resp.String())
	if len(observations) == 0 {
		return nil, fmt.Errorf("no IEM data available yet")
	}

	// Find max temp
	maxTemp := observations[0].Temp
	for _, o := range observations {
		if o.Temp > maxTemp {
			maxTemp = o.Temp
		}
	}

	// Parse threshold from question
	threshold := parseThresholdFromQuestion(market.Question)

	// Determine outcome
	outcome := "NO"
	if maxTemp > threshold {
		outcome = "YES"
	}

	return &ResolutionResult{
		Outcome:    outcome,
		Confidence: 1.0, // High confidence for IEM data
		TempF:      maxTemp,
		Source:     "IEM",
		Timestamp:  time.Now(),
	}, nil
}

type IEMObservation struct {
	Time time.Time
	Temp float64
}

func parseIEMData(csv string) []IEMObservation {
	lines := strings.Split(csv, "\n")
	if len(lines) < 2 {
		return nil
	}

	var observations []IEMObservation
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}

		// CSV format: station,valid,tmpf
		timestamp := parts[1]
		tempStr := parts[2]

		if tempStr == "null" || tempStr == "" {
			continue
		}

		t, err := time.Parse("2006-01-02 15:04", timestamp)
		if err != nil {
			continue
		}

		var temp float64
		fmt.Sscanf(tempStr, "%f", &temp)

		observations = append(observations, IEMObservation{
			Time: t,
			Temp: temp,
		})
	}

	return observations
}

func (r *Resolver) checkInternationalMarket(station string, market ResolverMarket) (*ResolutionResult, error) {
	// Fetch WU data
	wuData, err := r.wuScraper.GetDailyTemp(station, market.Date)
	if err != nil {
		return nil, fmt.Errorf("WU scrape error: %w", err)
	}

	// Optional: Validate with VC (check if within 0.5°C)
	vcTemp, _, err := r.vcClient.GetHighTemp(
		fmt.Sprintf("%s,%s", market.City, getCountry(market.City)),
		market.Date,
		market.Timezone,
	)

	// If VC available, check deviation
	if err == nil {
		deviationC := abs(wuData.HighTempC - (vcTemp-32)*5/9)
		if deviationC > 0.5 {
			// Log warning but continue with WU data
			fmt.Printf("⚠️  WARNING: VC/WU deviation %.1f°C for %s (using WU)\n", deviationC, market.City)
		}
	}

	// Parse threshold from question
	threshold := parseThresholdFromQuestion(market.Question)

	// Determine outcome
	outcome := "NO"
	if wuData.HighTempF > threshold {
		outcome = "YES"
	}

	return &ResolutionResult{
		Outcome:    outcome,
		Confidence: 0.95, // Slightly lower confidence for scraped data
		TempF:      wuData.HighTempF,
		Source:     "WU",
		Timestamp:  time.Now(),
	}, nil
}

func parseThresholdFromQuestion(question string) float64 {
	// Simple parsing - extract number followed by °F
	// Example: "exceed 50°F" -> 50.0
	question = strings.ToLower(question)

	// Look for patterns like "exceed 50" or "above 50" or "over 50"
	var threshold float64
	fmt.Sscanf(question, "%*s %f", &threshold)

	// Default threshold if parsing fails
	if threshold == 0 {
		threshold = 32.0 // Default freezing point
	}

	return threshold
}

func getCountry(city string) string {
	countryMap := map[string]string{
		"london":        "UK",
		"paris":         "France",
		"toronto":       "Canada",
		"seoul":         "South Korea",
		"buenos aires":  "Argentina",
		"ankara":        "Turkey",
		"sao paulo":     "Brazil",
		"wellington":    "New Zealand",
	}
	return countryMap[strings.ToLower(city)]
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
