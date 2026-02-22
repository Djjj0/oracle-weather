package resolvers

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/go-resty/resty/v2"
)

// IEMWeatherResolver uses Iowa Environmental Mesonet (matches Wunderground/Polymarket)
type IEMWeatherResolver struct {
	BaseResolver
	config *config.Config
	client *resty.Client
	cache  sync.Map
}

// NewIEMWeatherResolver creates a new IEM weather resolver
func NewIEMWeatherResolver(cfg *config.Config) *IEMWeatherResolver {
	return &IEMWeatherResolver{
		config: cfg,
		client: resty.New().SetTimeout(15 * time.Second),
		cache:  sync.Map{},
	}
}

// cityToAirport maps cities to their ICAO airport codes (Wunderground uses these)
var cityToAirport = map[string]string{
	"seattle":        "KSEA",
	"new york":       "KJFK",
	"los angeles":    "KLAX",
	"chicago":        "KORD",
	"houston":        "KIAH",
	"phoenix":        "KPHX",
	"philadelphia":   "KPHL",
	"san antonio":    "KSAT",
	"san diego":      "KSAN",
	"dallas":         "KDFW",
	"miami":          "KMIA",
	"atlanta":        "KATL",
	"boston":         "KBOS",
	"san francisco":  "KSFO",
	"denver":         "KDEN",
	"washington":     "KDCA",
	"nashville":      "KBNA",
	"london":         "EGLL", // Heathrow
	"paris":          "LFPG", // Charles de Gaulle
	"tokyo":          "RJTT", // Haneda
	"las vegas":      "KLAS",
	"portland":       "KPDX",
	"minneapolis":    "KMSP",
	"detroit":        "KDTW",
	"austin":         "KAUS",
	"memphis":        "KMEM",
	"charlotte":      "KCLT",
	"tampa":          "KTPA",
	"orlando":        "KMCO",
	"kansas city":    "KMCI",
	"buenos aires":   "SAEZ", // Ezeiza
	"seoul":          "RKSI", // Incheon
	"toronto":        "CYYZ", // Pearson
	"sydney":         "YSSY",
	"melbourne":      "YMML",
	"singapore":      "WSSS",
	"hong kong":      "VHHH",
	"beijing":        "ZBAA",
	"shanghai":       "ZSPD",
	"dubai":          "OMDB",
	"mumbai":         "VABB",
	"delhi":          "VIDP",
	"moscow":         "UUEE",
	"berlin":         "EDDB",
	"madrid":         "LEMD",
	"rome":           "LIRF",
	"amsterdam":      "EHAM",
	"barcelona":      "LEBL",
	"vienna":         "LOWW",
	"prague":         "LKPR",
	"stockholm":      "ESSA",
	"copenhagen":     "EKCH",
	"oslo":           "ENGM",
	"helsinki":       "EFHK",
	"brussels":       "EBBR",
	"lisbon":         "LPPT",
	"athens":         "LGAV",
	"warsaw":         "EPWA",
	"budapest":       "LHBP",
	"zurich":         "LSZH",
	"milan":          "LIMC",
	"munich":         "EDDM",
}

// CheckResolution checks weather using IEM ASOS data
func (w *IEMWeatherResolver) CheckResolution(market polymarket.Market) (*string, float64, error) {
	data, err := w.ParseMarketQuestion(market.Question)
	if err != nil {
		return nil, 0, err
	}

	// Check if resolution time has passed
	if data.Date.IsZero() || time.Now().Before(data.Date) {
		return nil, 0, nil
	}

	// Check cache
	cacheKey := fmt.Sprintf("%s_%s_%s", data.Location, data.Date.Format("2006-01-02"), data.Condition)
	if cached, ok := w.cache.Load(cacheKey); ok {
		result := cached.(CachedResult)
		return &result.Outcome, result.Confidence, nil
	}

	// Get airport code
	airportCode, ok := cityToAirport[strings.ToLower(data.Location)]
	if !ok {
		return nil, 0, fmt.Errorf("unknown city (no airport mapping): %s", data.Location)
	}

	// Determine if we need Celsius or Fahrenheit
	useCelsius := false
	if unit, ok := data.Extra["unit"]; ok && unit == "celsius" {
		useCelsius = true
	}

	// Get temperature from IEM
	highTemp, err := w.getIEMHighTemp(airportCode, data.Date, useCelsius)
	if err != nil {
		return nil, 0, fmt.Errorf("IEM API error: %w", err)
	}

	unitSymbol := "°F"
	if useCelsius {
		unitSymbol = "°C"
	}

	utils.Logger.Debugf("IEM Weather: %s (%s) on %s - High temp: %.1f%s",
		data.Location, airportCode, data.Date.Format("2006-01-02"), highTemp, unitSymbol)

	// Determine outcome
	outcome, confidence := w.determineOutcome(data, highTemp, unitSymbol)

	// Cache result
	w.cache.Store(cacheKey, CachedResult{Outcome: outcome, Confidence: confidence})

	return &outcome, confidence, nil
}

// getIEMHighTemp fetches daily high temperature from IEM ASOS API
func (w *IEMWeatherResolver) getIEMHighTemp(station string, date time.Time, celsius bool) (float64, error) {
	// IEM ASOS API endpoint
	url := fmt.Sprintf(
		"https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py?station=%s&data=tmpf&year1=%d&month1=%d&day1=%d&year2=%d&month2=%d&day2=%d&tz=UTC&format=onlycomma&latlon=no&elev=no&missing=null&trace=null&direct=no&report_type=3&report_type=4",
		station,
		date.Year(), int(date.Month()), date.Day(),
		date.Year(), int(date.Month()), date.Day(),
	)

	resp, err := w.client.R().Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch IEM data: %w", err)
	}

	if resp.IsError() {
		return 0, fmt.Errorf("IEM API returned error: %s", resp.Status())
	}

	// Parse CSV response
	lines := strings.Split(strings.TrimSpace(resp.String()), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("no data returned from IEM for station %s on %s", station, date.Format("2006-01-02"))
	}

	// Extract temperatures from CSV (skip header)
	var temps []float64
	for _, line := range lines[1:] {
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}

		// Temperature is in 3rd column (index 2)
		tempStr := strings.TrimSpace(parts[2])
		if tempStr == "" || tempStr == "null" || tempStr == "M" {
			continue
		}

		temp, err := strconv.ParseFloat(tempStr, 64)
		if err != nil {
			continue
		}

		// Sanity check (-100°F to 150°F)
		if temp >= -100 && temp <= 150 {
			temps = append(temps, temp)
		}
	}

	if len(temps) == 0 {
		return 0, fmt.Errorf("no valid temperature readings for %s on %s", station, date.Format("2006-01-02"))
	}

	// Get daily high (max temperature)
	highTemp := temps[0]
	for _, t := range temps {
		if t > highTemp {
			highTemp = t
		}
	}

	// Convert to Celsius if needed
	if celsius {
		highTemp = (highTemp - 32) * 5 / 9
	}

	return highTemp, nil
}

// determineOutcome determines market outcome based on temperature
func (w *IEMWeatherResolver) determineOutcome(data *MarketData, temp float64, unitSymbol string) (string, float64) {
	// Round to nearest integer for Polymarket resolution
	roundedTemp := math.Round(temp)

	switch data.Condition {
	case "temperature_exact":
		if roundedTemp == data.Threshold {
			utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s, expected=%.0f%s → Yes",
				data.Location, temp, unitSymbol, data.Threshold, unitSymbol)
			return "Yes", 0.98 // Very high confidence with IEM
		}
		utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s, expected=%.0f%s → No",
			data.Location, temp, unitSymbol, data.Threshold, unitSymbol)
		return "No", 0.98

	case "temperature_range":
		tempLow := data.Extra["temp_low"].(float64)
		tempHigh := data.Extra["temp_high"].(float64)
		if roundedTemp >= tempLow && roundedTemp <= tempHigh {
			utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s, range=%.0f-%.0f%s → Yes",
				data.Location, temp, unitSymbol, tempLow, tempHigh, unitSymbol)
			return "Yes", 0.98
		}
		utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s, range=%.0f-%.0f%s → No",
			data.Location, temp, unitSymbol, tempLow, tempHigh, unitSymbol)
		return "No", 0.98

	case "temperature_above":
		if roundedTemp >= data.Threshold {
			utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s ≥ %.0f%s → Yes",
				data.Location, temp, unitSymbol, data.Threshold, unitSymbol)
			return "Yes", 0.98
		}
		utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s < %.0f%s → No",
			data.Location, temp, unitSymbol, data.Threshold, unitSymbol)
		return "No", 0.98

	case "temperature_below":
		if roundedTemp <= data.Threshold {
			return "Yes", 0.98
		}
		return "No", 0.98

	default:
		return "No", 0.50
	}
}

// ParseMarketQuestion delegates to base weather resolver
func (w *IEMWeatherResolver) ParseMarketQuestion(question string) (*MarketData, error) {
	// Use the existing weather resolver's parsing logic
	wr := NewWeatherResolver(w.config)
	return wr.ParseMarketQuestion(question)
}
