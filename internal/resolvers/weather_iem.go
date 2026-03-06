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
	pkgweather "github.com/djbro/oracle-weather/pkg/weather"
	"github.com/go-resty/resty/v2"
)

// IEMWeatherResolver uses Iowa Environmental Mesonet (matches Wunderground/Polymarket)
type IEMWeatherResolver struct {
	BaseResolver
	config     *config.Config
	client     *resty.Client
	cache      sync.Map
	learningDB *pkgweather.LearningDB
}

// NewIEMWeatherResolver creates a new IEM weather resolver
func NewIEMWeatherResolver(cfg *config.Config) *IEMWeatherResolver {
	learningDB, err := pkgweather.NewLearningDB("./data/learning.db")
	if err != nil {
		utils.Logger.Warnf("IEM resolver: could not open learning DB (peak times will use defaults): %v", err)
		learningDB = nil
	}
	return &IEMWeatherResolver{
		config:     cfg,
		client:     resty.New().SetTimeout(15 * time.Second),
		cache:      sync.Map{},
		learningDB: learningDB,
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

// CheckResolution checks weather using IEM ASOS data.
//
// Strategy:
//   - IEM updates observations each hour, shortly after the hour ends.
//   - The learning DB tracks when the daily high is typically reached per city.
//   - YES bets: place as soon as the running IEM high already exceeds the threshold.
//   - NO bets: place once we are past the city's typical peak hour AND the running
//     high is clearly below the threshold (it won't recover).
//   - Rain markets: require full day (11 PM gate) since rain can occur any time.
// Name returns the human-readable name for this resolver.
func (w *IEMWeatherResolver) Name() string { return "IEM ASOS Weather" }

func (w *IEMWeatherResolver) CheckResolution(market polymarket.Market) (*string, float64, error) {
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

	// Rain needs full day of data
	if data.Condition == "rain" || data.Condition == "precipitation" {
		localDate := data.Date.In(loc)
		rainGate := time.Date(localDate.Year(), localDate.Month(), localDate.Day(), 23, 0, 0, 0, loc)
		now := time.Now().In(loc)
		if now.Before(rainGate) {
			utils.Logger.Debugf("⏳ Rain market not ready: %s on %s (%.1fh until 11 PM gate)",
				data.Location, data.Date.Format("2006-01-02"), rainGate.Sub(now).Hours())
			return nil, 0, nil
		}
	}

	// Get airport code
	airportCode, ok := cityToAirport[strings.ToLower(data.Location)]
	if !ok {
		return nil, 0, fmt.Errorf("unknown city (no airport mapping): %s", data.Location)
	}

	useCelsius := false
	if unit, ok := data.Extra["unit"]; ok && unit == "celsius" {
		useCelsius = true
	}
	unitSymbol := "°F"
	if useCelsius {
		unitSymbol = "°C"
	}

	// Check cache (short TTL - data updates hourly so don't cache too long)
	cacheKey := fmt.Sprintf("%s_%s_%s", data.Location, data.Date.Format("2006-01-02"), data.Condition)
	if cached, ok := w.cache.Load(cacheKey); ok {
		result := cached.(CachedResult)
		return &result.Outcome, result.Confidence, nil
	}

	// Rain/precipitation markets: use dedicated precipitation endpoint
	if data.Condition == "rain" || data.Condition == "precipitation" {
		totalPrecip, err := w.getIEMPrecip(airportCode, data.Date)
		if err != nil {
			return nil, 0, fmt.Errorf("IEM precip API error: %w", err)
		}

		localDate := data.Date.In(loc)
		var outcome string
		if totalPrecip > 0 {
			outcome = "Yes"
		} else {
			outcome = "No"
		}
		// High confidence since we have the full day of data (gate already passed above)
		confidence := 0.92

		utils.Logger.Infof("✅ IEM rain resolved: %s on %s | Total precip: %.4f in | Outcome: %s | Confidence: %.0f%%",
			data.Location, localDate.Format("2006-01-02"), totalPrecip, outcome, confidence*100)

		w.cache.Store(cacheKey, CachedResult{Outcome: outcome, Confidence: confidence})
		return &outcome, confidence, nil
	}

	// Fetch the current running high from IEM (max of all observations so far today)
	runningHigh, err := w.getIEMHighTemp(airportCode, data.Date, useCelsius)
	if err != nil {
		return nil, 0, fmt.Errorf("IEM API error: %w", err)
	}

	now := time.Now().In(loc)
	localDate := data.Date.In(loc)
	currentHour := float64(now.Hour()) + float64(now.Minute())/60.0

	utils.Logger.Debugf("IEM running high: %s (%s) on %s = %.1f%s at %.1f local hour",
		data.Location, airportCode, data.Date.Format("2006-01-02"), runningHigh, unitSymbol, currentHour)

	// Enforce hard minimum trading hour regardless of learning DB data.
	// This prevents early NO bets on days when the temperature hasn't peaked yet.
	hardFloor := float64(cityMinTradingHour(data.Location))
	if currentHour < hardFloor {
		utils.Logger.Infof("⏳ Too early to trade %s: %.1f local hour < %.0f gate",
			data.Location, currentHour, hardFloor)
		return nil, 0, nil
	}

	// Determine typical peak hour for this city from learning DB
	// Falls back to 15.0 (3 PM) if not yet in DB
	typicalPeakHour := w.getTypicalPeakHour(data.Location, loc)

	// Check if YES is already confirmed — running high already exceeds threshold.
	// We can bet YES any time of day since the high can only go up.
	outcome, confidence := w.determineOutcomeWithPeak(data, runningHigh, unitSymbol, currentHour, typicalPeakHour)
	if outcome == "" {
		// Not yet actionable
		utils.Logger.Debugf("⏳ Not yet actionable: %s | Running high: %.1f%s | Now: %.1f local | Typical peak: %.1f",
			data.Location, runningHigh, unitSymbol, currentHour, typicalPeakHour)
		return nil, 0, nil
	}

	utils.Logger.Infof("✅ IEM resolved: %s on %s | Running high: %.1f%s | Outcome: %s | Confidence: %.0f%%",
		data.Location, localDate.Format("2006-01-02"), runningHigh, unitSymbol, outcome, confidence*100)

	w.cache.Store(cacheKey, CachedResult{Outcome: outcome, Confidence: confidence})
	return &outcome, confidence, nil
}

// getTypicalPeakHour returns the typical hour (local time) when the daily high is reached
// for a given city, using the learning DB. Falls back to 15.0 (3 PM) if no data.
func (w *IEMWeatherResolver) getTypicalPeakHour(city string, loc *time.Location) float64 {
	if w.learningDB == nil {
		return 15.0
	}
	stats, err := w.learningDB.GetCityStats(strings.ToLower(city))
	if err != nil || stats.TotalMarkets < 5 {
		return 15.0 // Not enough data yet
	}
	return stats.AvgHighTempHour
}

// determineOutcomeWithPeak decides whether to bet YES or NO based on:
//   - The current running IEM high
//   - Whether we are past the typical peak hour (for NO bets)
func (w *IEMWeatherResolver) determineOutcomeWithPeak(data *MarketData, runningHigh float64, unitSymbol string, currentHour, typicalPeakHour float64) (string, float64) {
	roundedHigh := math.Round(runningHigh)

	// Add 1 hour buffer past typical peak before allowing NO bets
	// (gives IEM time to record the actual peak observation)
	pastPeak := currentHour >= typicalPeakHour+1.0

	switch data.Condition {
	case "temperature_above":
		if roundedHigh >= data.Threshold {
			// Already exceeded — safe YES bet any time
			return "Yes", marginToConfidence(runningHigh-data.Threshold)
		}
		if pastPeak {
			// Past peak and still below — won't recover, bet NO
			margin := data.Threshold - runningHigh
			// Extra confidence the further past peak we are
			peakMarginBonus := math.Min((currentHour-typicalPeakHour-1.0)*0.05, 0.10)
			return "No", math.Min(marginToConfidence(margin)+peakMarginBonus, 0.98)
		}

	case "temperature_below":
		if roundedHigh > data.Threshold {
			// Already exceeded ceiling — safe NO bet any time
			return "No", marginToConfidence(runningHigh-data.Threshold)
		}
		if pastPeak {
			margin := data.Threshold - runningHigh
			peakMarginBonus := math.Min((currentHour-typicalPeakHour-1.0)*0.05, 0.10)
			return "Yes", math.Min(marginToConfidence(margin)+peakMarginBonus, 0.98)
		}

	case "temperature_exact":
		if roundedHigh == data.Threshold {
			if pastPeak {
				// On the number and past peak — high confidence YES
				margin := 0.5 - math.Abs(runningHigh-data.Threshold)
				if margin < 0 {
					margin = 0
				}
				return "Yes", marginToConfidence(margin)
			}
			// On the number but not past peak — could still go higher, wait
			return "", 0
		}
		if pastPeak {
			// Past peak and not on target number — bet NO
			margin := math.Abs(runningHigh - data.Threshold)
			return "No", marginToConfidence(margin)
		}

	case "temperature_range":
		tempLow := data.Extra["temp_low"].(float64)
		tempHigh := data.Extra["temp_high"].(float64)
		if roundedHigh >= tempLow && roundedHigh <= tempHigh {
			if pastPeak {
				// In range and past peak — YES
				margin := math.Min(runningHigh-tempLow, tempHigh-runningHigh)
				return "Yes", marginToConfidence(margin)
			}
			// In range but not past peak — could still go higher and exit range
			return "", 0
		}
		if roundedHigh > tempHigh {
			// Already above range ceiling — safe NO any time
			return "No", marginToConfidence(runningHigh-tempHigh)
		}
		if pastPeak && roundedHigh < tempLow {
			// Past peak and below floor — won't reach range, bet NO
			margin := tempLow - runningHigh
			peakMarginBonus := math.Min((currentHour-typicalPeakHour-1.0)*0.05, 0.10)
			return "No", math.Min(marginToConfidence(margin)+peakMarginBonus, 0.98)
		}
	}

	return "", 0
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

// marginToConfidence converts degrees of margin from a threshold to a confidence value.
// The further the actual temperature is from the decision boundary, the more confident we are.
func marginToConfidence(marginDegrees float64) float64 {
	switch {
	case marginDegrees >= 3.0:
		return 0.98
	case marginDegrees >= 2.0:
		return 0.95
	case marginDegrees >= 1.0:
		return 0.90
	case marginDegrees >= 0.5:
		return 0.80
	default:
		// Very close to boundary — rounding or station variance could flip the result
		return 0.70
	}
}

// determineOutcome determines market outcome and confidence based on temperature.
// Confidence reflects how far the actual temp is from the decision boundary —
// e.g. 75°F on "above 50°F" is 0.98, but 50.2°F on the same market is 0.70.
func (w *IEMWeatherResolver) determineOutcome(data *MarketData, temp float64, unitSymbol string) (string, float64) {
	roundedTemp := math.Round(temp)

	switch data.Condition {
	case "temperature_exact":
		// Margin = how far from the rounding boundary (max 0.5 when exactly on threshold)
		margin := 0.5 - math.Abs(temp-data.Threshold)
		if margin < 0 {
			margin = 0
		}
		confidence := marginToConfidence(margin)
		if roundedTemp == data.Threshold {
			utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s, expected=%.0f%s → Yes (confidence: %.0f%%, margin: %.1f°)",
				data.Location, temp, unitSymbol, data.Threshold, unitSymbol, confidence*100, margin)
			return "Yes", confidence
		}
		utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s, expected=%.0f%s → No (confidence: %.0f%%, margin: %.1f°)",
			data.Location, temp, unitSymbol, data.Threshold, unitSymbol, confidence*100, margin)
		return "No", confidence

	case "temperature_range":
		tempLow := data.Extra["temp_low"].(float64)
		tempHigh := data.Extra["temp_high"].(float64)
		if roundedTemp >= tempLow && roundedTemp <= tempHigh {
			// Margin = distance from nearest edge of the range
			margin := math.Min(temp-tempLow, tempHigh-temp)
			confidence := marginToConfidence(margin)
			utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s, range=%.0f-%.0f%s → Yes (confidence: %.0f%%, margin: %.1f°)",
				data.Location, temp, unitSymbol, tempLow, tempHigh, unitSymbol, confidence*100, margin)
			return "Yes", confidence
		}
		// Outside range — margin = distance from nearest edge
		var margin float64
		if temp < tempLow {
			margin = tempLow - temp
		} else {
			margin = temp - tempHigh
		}
		confidence := marginToConfidence(margin)
		utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s, range=%.0f-%.0f%s → No (confidence: %.0f%%, margin: %.1f°)",
			data.Location, temp, unitSymbol, tempLow, tempHigh, unitSymbol, confidence*100, margin)
		return "No", confidence

	case "temperature_above":
		if roundedTemp >= data.Threshold {
			margin := temp - data.Threshold
			confidence := marginToConfidence(margin)
			utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s ≥ %.0f%s → Yes (confidence: %.0f%%, margin: %.1f°)",
				data.Location, temp, unitSymbol, data.Threshold, unitSymbol, confidence*100, margin)
			return "Yes", confidence
		}
		margin := data.Threshold - temp
		confidence := marginToConfidence(margin)
		utils.Logger.Infof("IEM Weather resolved: %s high=%.1f%s < %.0f%s → No (confidence: %.0f%%, margin: %.1f°)",
			data.Location, temp, unitSymbol, data.Threshold, unitSymbol, confidence*100, margin)
		return "No", confidence

	case "temperature_below":
		if roundedTemp <= data.Threshold {
			margin := data.Threshold - temp
			confidence := marginToConfidence(margin)
			return "Yes", confidence
		}
		margin := temp - data.Threshold
		confidence := marginToConfidence(margin)
		return "No", confidence

	default:
		return "No", 0.50
	}
}

// getIEMPrecip fetches hourly precipitation (p01i) from IEM ASOS for a given station/date
// and returns the total precipitation in inches for that calendar day (UTC).
// A return value > 0 means it rained; 0 means no measurable precipitation.
func (w *IEMWeatherResolver) getIEMPrecip(station string, date time.Time) (float64, error) {
	url := fmt.Sprintf(
		"https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py?station=%s&data=p01i&year1=%d&month1=%d&day1=%d&year2=%d&month2=%d&day2=%d&tz=UTC&format=onlycomma&latlon=no&elev=no&missing=null&trace=null&direct=no&report_type=3&report_type=4",
		station,
		date.Year(), int(date.Month()), date.Day(),
		date.Year(), int(date.Month()), date.Day(),
	)

	resp, err := w.client.R().Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch IEM precip data: %w", err)
	}
	if resp.IsError() {
		return 0, fmt.Errorf("IEM precip API returned error: %s", resp.Status())
	}

	lines := strings.Split(strings.TrimSpace(resp.String()), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("no precip data returned from IEM for station %s on %s", station, date.Format("2006-01-02"))
	}

	// CSV columns: station, valid, p01i
	// Sum all valid hourly precip values; skip null/missing
	var totalPrecip float64
	validReadings := 0
	for _, line := range lines[1:] {
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		valStr := strings.TrimSpace(parts[2])
		if valStr == "" || valStr == "null" || valStr == "M" {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		if val >= 0 { // negative would be bogus
			totalPrecip += val
			validReadings++
		}
	}

	if validReadings == 0 {
		return 0, fmt.Errorf("no valid precip readings for %s on %s", station, date.Format("2006-01-02"))
	}

	utils.Logger.Debugf("IEM precip: %s on %s = %.4f in total (%d readings)", station, date.Format("2006-01-02"), totalPrecip, validReadings)
	return totalPrecip, nil
}

// ParseMarketQuestion delegates to base weather resolver
func (w *IEMWeatherResolver) ParseMarketQuestion(question string) (*MarketData, error) {
	// Use the existing weather resolver's parsing logic
	wr := NewWeatherResolver(w.config)
	return wr.ParseMarketQuestion(question)
}
