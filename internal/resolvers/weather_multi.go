package resolvers

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/go-resty/resty/v2"
)

// MultiSourceWeatherResolver validates weather data via IEM ASOS (Polymarket's official source).
// Kept for compatibility — the factory now uses IEMWeatherResolver directly.
type MultiSourceWeatherResolver struct {
	BaseResolver
	config *config.Config
	client *resty.Client
	cache  sync.Map
}

// WeatherSource represents a single weather API source result
type WeatherSource struct {
	Name        string
	Temperature float64
	Success     bool
	Error       error
}

// NewMultiSourceWeatherResolver creates a new multi-source weather resolver
func NewMultiSourceWeatherResolver(cfg *config.Config) *MultiSourceWeatherResolver {
	return &MultiSourceWeatherResolver{
		config: cfg,
		client: resty.New().SetTimeout(10 * time.Second),
		cache:  sync.Map{},
	}
}

// Name returns the human-readable name for this resolver.
func (w *MultiSourceWeatherResolver) Name() string { return "Multi-Source Weather (IEM)" }

// CheckResolution validates weather via IEM ASOS only (Polymarket's official source)
func (w *MultiSourceWeatherResolver) CheckResolution(market polymarket.Market) (*string, float64, error) {
	data, err := w.ParseMarketQuestion(market.Question)
	if err != nil {
		return nil, 0, err
	}

	if data.Date.IsZero() {
		return nil, 0, nil
	}

	tzStr := getTimezone(data.Location)
	loc, err := time.LoadLocation(tzStr)
	if err != nil {
		utils.Logger.Warnf("Failed to load timezone %s for %s, using UTC: %v", tzStr, data.Location, err)
		loc = time.UTC
	}

	// Minimum local hour before we allow trading — temperatures must have had
	// time to peak. Rain needs full day. Temperature markets need post-peak data.
	var minHour int
	switch data.Condition {
	case "rain", "precipitation":
		minHour = 23
	default:
		minHour = cityMinTradingHour(data.Location)
	}

	localDate := data.Date.In(loc)
	gateTime := time.Date(localDate.Year(), localDate.Month(), localDate.Day(), minHour, 0, 0, 0, loc)

	now := time.Now().In(loc)
	if now.Before(gateTime) {
		utils.Logger.Infof("⏳ Too early to trade %s: now %s local, gate is %s (%.1fh remaining)",
			data.Location,
			now.Format("15:04 MST"),
			gateTime.Format("15:04 MST"),
			gateTime.Sub(now).Hours())
		return nil, 0, nil
	}

	cacheKey := fmt.Sprintf("%s_%s_%s", data.Location, data.Date.Format("2006-01-02"), data.Condition)
	if cached, ok := w.cache.Load(cacheKey); ok {
		result := cached.(CachedResult)
		return &result.Outcome, result.Confidence, nil
	}

	airportCode, hasAirport := cityToAirport[strings.ToLower(data.Location)]
	if !hasAirport {
		return nil, 0, fmt.Errorf("no airport mapping for city: %s (IEM required)", data.Location)
	}

	useCelsius := false
	if unit, ok := data.Extra["unit"]; ok && unit == "celsius" {
		useCelsius = true
	}

	temp, err := w.getIEMTemp(airportCode, data.Date, useCelsius)
	if err != nil {
		return nil, 0, fmt.Errorf("IEM/Wunderground API failed - cannot trade without official data: %w", err)
	}

	unitStr := "fahrenheit"
	if useCelsius {
		unitStr = "celsius"
	}
	utils.Logger.Infof("✅ IEM/Wunderground (Polymarket's OFFICIAL source): %s = %.1f°%s",
		data.Location, temp, map[string]string{"fahrenheit": "F", "celsius": "C"}[unitStr])

	outcome, confidence := w.determineOutcome(data, temp, unitStr)
	w.cache.Store(cacheKey, CachedResult{Outcome: outcome, Confidence: confidence})

	return &outcome, confidence, nil
}

// getIEMTemp fetches the daily high temperature from IEM ASOS
func (w *MultiSourceWeatherResolver) getIEMTemp(station string, date time.Time, celsius bool) (float64, error) {
	url := fmt.Sprintf(
		"https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py?station=%s&data=tmpf&year1=%d&month1=%d&day1=%d&year2=%d&month2=%d&day2=%d&tz=UTC&format=onlycomma&latlon=no&elev=no&missing=null&trace=null&direct=no&report_type=3&report_type=4",
		station,
		date.Year(), int(date.Month()), date.Day(),
		date.Year(), int(date.Month()), date.Day(),
	)

	resp, err := w.client.R().Get(url)
	if err != nil {
		return 0, err
	}

	lines := strings.Split(strings.TrimSpace(resp.String()), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("no data from IEM")
	}

	var temps []float64
	for _, line := range lines[1:] {
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		tempStr := strings.TrimSpace(parts[2])
		if tempStr == "" || tempStr == "null" {
			continue
		}
		if temp, err := strconv.ParseFloat(tempStr, 64); err == nil && temp >= -100 && temp <= 150 {
			temps = append(temps, temp)
		}
	}

	if len(temps) == 0 {
		return 0, fmt.Errorf("no valid temps from IEM for station %s", station)
	}

	highTemp := temps[0]
	for _, t := range temps {
		if t > highTemp {
			highTemp = t
		}
	}

	if celsius {
		highTemp = (highTemp - 32) * 5 / 9
	}

	return highTemp, nil
}

// determineOutcome determines market outcome based on IEM temperature
func (w *MultiSourceWeatherResolver) determineOutcome(data *MarketData, temp float64, unit string) (string, float64) {
	unitSymbol := "°F"
	if unit == "celsius" {
		unitSymbol = "°C"
	}

	roundedTemp := float64(int(temp + 0.5))

	switch data.Condition {
	case "temperature_exact":
		if roundedTemp == data.Threshold {
			utils.Logger.Infof("Weather resolved: %s high=%.1f%s, expected=%.0f%s → Yes",
				data.Location, temp, unitSymbol, data.Threshold, unitSymbol)
			return "Yes", 0.95
		}
		utils.Logger.Infof("Weather resolved: %s high=%.1f%s, expected=%.0f%s → No",
			data.Location, temp, unitSymbol, data.Threshold, unitSymbol)
		return "No", 0.95

	case "temperature_range":
		tempLow := data.Extra["temp_low"].(float64)
		tempHigh := data.Extra["temp_high"].(float64)
		if roundedTemp >= tempLow && roundedTemp <= tempHigh {
			return "Yes", 0.95
		}
		return "No", 0.95

	case "temperature_above":
		if roundedTemp >= data.Threshold {
			return "Yes", 0.95
		}
		return "No", 0.95

	case "temperature_below":
		if roundedTemp <= data.Threshold {
			return "Yes", 0.95
		}
		return "No", 0.95

	default:
		return "No", 0.50
	}
}

// cityMinTradingHour returns the earliest local hour at which we allow trading
// a temperature market for a given city. This is set conservatively — 1-2 hours
// after the typical daily high is reached — to avoid acting on incomplete data.
func cityMinTradingHour(city string) int {
	hours := map[string]int{
		// US cities — IEM data finalises by early evening; peaks ~1-4pm local
		"chicago":       19, // 7pm CST (from DEPLOYMENT_READY)
		"seattle":       17, // 5pm PST
		"new york":      20, // 8pm EST
		"miami":         20, // 8pm EST
		"dallas":        19, // 7pm CST
		"atlanta":       20, // 8pm EST
		"los angeles":   18, // 6pm PST
		"houston":       19, // 7pm CST
		"phoenix":       19, // 7pm MST
		"denver":        19, // 7pm MST
		"boston":        20, // 8pm EST
		"washington":    20, // 8pm EST
		"las vegas":     18, // 6pm PST
		"portland":      17, // 5pm PST
		"minneapolis":   19, // 7pm CST
		"san francisco": 18, // 6pm PST

		// Europe — peaks 2-4pm local; trade from 6pm to be safe
		"london":    18, // 6pm GMT/BST
		"paris":     18, // 6pm CET/CEST
		"berlin":    18, // 6pm CET/CEST
		"madrid":    19, // 7pm CET/CEST (later peak due to latitude/climate)
		"rome":      18, // 6pm CET/CEST
		"amsterdam": 18,
		"brussels":  18,
		"vienna":    18,
		"prague":    18,
		"stockholm": 18,
		"oslo":      18,
		"helsinki":  18,
		"copenhagen": 18,
		"lisbon":    18,
		"athens":    19,
		"warsaw":    18,
		"budapest":  18,
		"zurich":    18,
		"milan":     18,
		"munich":    18,
		"moscow":    18,
		"barcelona": 19,

		// Other international
		"toronto":      20, // 8pm EST
		"buenos aires": 19, // 7pm ART
		"seoul":        18, // 6pm KST
		"tokyo":        18, // 6pm JST
		"sydney":       18,
		"singapore":    18,
		"hong kong":    18,
		"dubai":        19,
		"mumbai":       18,
	}
	if h, ok := hours[strings.ToLower(city)]; ok {
		return h
	}
	return 19 // safe default: 7pm local
}

// ParseMarketQuestion delegates to the base weather resolver's parsing logic
func (w *MultiSourceWeatherResolver) ParseMarketQuestion(question string) (*MarketData, error) {
	wr := NewWeatherResolver(w.config)
	return wr.ParseMarketQuestion(question)
}
