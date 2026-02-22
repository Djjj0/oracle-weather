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
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/go-resty/resty/v2"
)

// WeatherResolver resolves weather-related markets
type WeatherResolver struct {
	BaseResolver
	config *config.Config
	client *resty.Client
	cache  sync.Map // Thread-safe cache
}

// OpenMeteoResponse represents Open-Meteo historical API response
type OpenMeteoResponse struct {
	Daily struct {
		Time           []string  `json:"time"`
		TempMax        []float64 `json:"temperature_2m_max"`
		TempMin        []float64 `json:"temperature_2m_min"`
		PrecipSum      []float64 `json:"precipitation_sum"`
		RainSum        []float64 `json:"rain_sum"`
		SnowfallSum    []float64 `json:"snowfall_sum"`
	} `json:"daily"`
}

// cityCoordinates maps city names to lat/lon for Open-Meteo
var cityCoordinates = map[string][2]float64{
	"seattle":       {47.6062, -122.3321},
	"new york":      {40.7128, -74.0060},
	"los angeles":   {34.0522, -118.2437},
	"chicago":       {41.8781, -87.6298},
	"houston":       {29.7604, -95.3698},
	"phoenix":       {33.4484, -112.0740},
	"philadelphia":  {39.9526, -75.1652},
	"san antonio":   {29.4241, -98.4936},
	"san diego":     {32.7157, -117.1611},
	"dallas":        {32.7767, -96.7970},
	"miami":         {25.7617, -80.1918},
	"atlanta":       {33.7490, -84.3880},
	"boston":         {42.3601, -71.0589},
	"san francisco": {37.7749, -122.4194},
	"denver":        {39.7392, -104.9903},
	"washington":    {38.9072, -77.0369},
	"nashville":     {36.1627, -86.7816},
	"london":        {51.5074, -0.1278},
	"paris":         {48.8566, 2.3522},
	"tokyo":         {35.6762, 139.6503},
	"las vegas":     {36.1699, -115.1398},
	"portland":      {45.5152, -122.6784},
	"minneapolis":   {44.9778, -93.2650},
	"detroit":       {42.3314, -83.0458},
	"austin":        {30.2672, -97.7431},
	"memphis":       {35.1495, -90.0490},
	"charlotte":     {35.2271, -80.8431},
	"tampa":         {27.9506, -82.4572},
	"orlando":       {28.5383, -81.3792},
	"kansas city":   {39.0997, -94.5786},
	"buenos aires":  {-34.6037, -58.3816},
	"seoul":         {37.5665, 126.9780},
	"toronto":       {43.6532, -79.3832},
	"sydney":        {-33.8688, 151.2093},
	"melbourne":     {-37.8136, 144.9631},
	"singapore":     {1.3521, 103.8198},
	"hong kong":     {22.3193, 114.1694},
	"beijing":       {39.9042, 116.4074},
	"shanghai":      {31.2304, 121.4737},
	"dubai":         {25.2048, 55.2708},
	"mumbai":        {19.0760, 72.8777},
	"delhi":         {28.7041, 77.1025},
	"moscow":        {55.7558, 37.6173},
	"berlin":        {52.5200, 13.4050},
	"madrid":        {40.4168, -3.7038},
	"rome":          {41.9028, 12.4964},
	"amsterdam":     {52.3676, 4.9041},
	"barcelona":     {41.3851, 2.1734},
	"vienna":        {48.2082, 16.3738},
	"prague":        {50.0755, 14.4378},
	"stockholm":     {59.3293, 18.0686},
	"copenhagen":    {55.6761, 12.5683},
	"oslo":          {59.9139, 10.7522},
	"helsinki":      {60.1699, 24.9384},
	"brussels":      {50.8503, 4.3517},
	"lisbon":        {38.7223, -9.1393},
	"athens":        {37.9838, 23.7275},
	"warsaw":        {52.2297, 21.0122},
	"budapest":      {47.4979, 19.0402},
	"zurich":        {47.3769, 8.5417},
	"milan":         {45.4642, 9.1900},
	"munich":        {48.1351, 11.5820},
	"frankfurt":     {50.1109, 8.6821},
}

// NewWeatherResolver creates a new weather resolver
func NewWeatherResolver(cfg *config.Config) *WeatherResolver {
	client := resty.New()
	client.SetTimeout(10 * time.Second)

	resolver := &WeatherResolver{
		config: cfg,
		client: client,
	}
	resolver.SetConfidence(0.95)

	return resolver
}

// ParseMarketQuestion extracts weather data from question
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
		dateStr := strings.TrimSpace(matches[3])
		if parsedDate, err := parseDate(dateStr); err == nil {
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
		dateStr := strings.TrimSpace(matches[3])
		if parsedDate, err := parseDate(dateStr); err == nil {
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
		dateStr := strings.TrimSpace(matches[3])
		if parsedDate, err := parseDate(dateStr); err == nil {
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
		dateStr := strings.TrimSpace(matches[4])
		if parsedDate, err := parseDate(dateStr); err == nil {
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
		dateStr := strings.TrimSpace(matches[3])
		if parsedDate, err := parseDate(dateStr); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// Pattern 3: "Will the highest temperature in [city] be [X]°F or lower on [date]?"
	tempLowerPattern := regexp.MustCompile(`(?i)will the highest temperature in ([a-z\s]+?) be (\d+)°?f or lower on ([a-z]+\s+\d+)`)
	if matches := tempLowerPattern.FindStringSubmatch(question); len(matches) > 3 {
		data.Location = strings.TrimSpace(matches[1])
		data.Condition = "temperature_below"
		data.Threshold, _ = strconv.ParseFloat(matches[2], 64)
		dateStr := strings.TrimSpace(matches[3])
		if parsedDate, err := parseDate(dateStr); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	// Pattern 4: "Will it rain in [city] on [date]?"
	rainPattern := regexp.MustCompile(`(?i)will it rain in ([a-z\s]+) on ([a-z0-9\s,]+)`)
	if matches := rainPattern.FindStringSubmatch(questionLower); len(matches) > 2 {
		data.Location = strings.TrimSpace(matches[1])
		data.Condition = "rain"
		dateStr := strings.TrimSpace(matches[2])
		if parsedDate, err := parseDate(dateStr); err == nil {
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
			// Try to extract date from question
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
		dateStr := strings.TrimSpace(matches[4])
		if parsedDate, err := parseDate(dateStr); err == nil {
			data.Date = parsedDate
		}
		return data, nil
	}

	return nil, fmt.Errorf("could not parse weather question")
}

// CheckResolution checks weather data and returns outcome with confidence
func (w *WeatherResolver) CheckResolution(market polymarket.Market) (*string, float64, error) {
	data, err := w.ParseMarketQuestion(market.Question)
	if err != nil {
		return nil, 0, err
	}

	// Check if resolution time has passed
	if data.Date.IsZero() {
		return nil, 0, fmt.Errorf("could not determine date")
	}
	if time.Now().Before(data.Date) {
		return nil, 0, nil // Not yet resolvable
	}

	// Check cache first
	cacheKey := fmt.Sprintf("%s_%s_%s", data.Location, data.Date.Format("2006-01-02"), data.Condition)
	if cached, ok := w.cache.Load(cacheKey); ok {
		result := cached.(CachedResult)
		return &result.Outcome, result.Confidence, nil
	}

	// Get coordinates for the city
	coords, ok := cityCoordinates[strings.ToLower(data.Location)]
	if !ok {
		return nil, 0, fmt.Errorf("unknown city: %s", data.Location)
	}

	// Use Open-Meteo historical API (free, no key needed)
	dateStr := data.Date.Format("2006-01-02")

	// Determine temperature unit
	tempUnit := "fahrenheit"
	unitSymbol := "°F"
	if unit, ok := data.Extra["unit"]; ok && unit == "celsius" {
		tempUnit = "celsius"
		unitSymbol = "°C"
	}

	var meteoResp OpenMeteoResponse
	resp, err := w.client.R().
		SetResult(&meteoResp).
		Get(fmt.Sprintf(
			"https://archive-api.open-meteo.com/v1/archive?latitude=%.4f&longitude=%.4f&start_date=%s&end_date=%s&daily=temperature_2m_max,temperature_2m_min,precipitation_sum,rain_sum,snowfall_sum&temperature_unit=%s&timezone=America%%2FLos_Angeles",
			coords[0], coords[1], dateStr, dateStr, tempUnit,
		))

	if err != nil {
		return nil, 0, fmt.Errorf("Open-Meteo API error: %w", err)
	}

	if resp.IsError() {
		return nil, 0, fmt.Errorf("Open-Meteo API returned error: %s", resp.Status())
	}

	if len(meteoResp.Daily.TempMax) == 0 {
		return nil, 0, fmt.Errorf("no weather data returned for %s on %s", data.Location, dateStr)
	}

	highTemp := meteoResp.Daily.TempMax[0]
	utils.Logger.Debugf("Weather: %s on %s - High temp: %.1f%s", data.Location, dateStr, highTemp, unitSymbol)

	// Determine outcome based on condition
	var outcome string
	confidence := 0.95

	switch data.Condition {
	case "temperature_exact":
		// Round high temp to nearest integer for exact comparison
		roundedHigh := float64(int(highTemp + 0.5))
		if roundedHigh == data.Threshold {
			outcome = "Yes"
		} else {
			outcome = "No"
		}
		utils.Logger.Infof("Weather resolved: %s high=%.1f%s, expected=%.0f%s → %s",
			data.Location, highTemp, unitSymbol, data.Threshold, unitSymbol, outcome)

	case "temperature_range":
		tempLow := data.Extra["temp_low"].(float64)
		tempHigh := data.Extra["temp_high"].(float64)
		// Round high temp to nearest integer for comparison
		roundedHigh := float64(int(highTemp + 0.5))
		if roundedHigh >= tempLow && roundedHigh <= tempHigh {
			outcome = "Yes"
		} else {
			outcome = "No"
		}
		utils.Logger.Infof("Weather resolved: %s high=%.1f%s, range=%.0f-%.0f%s → %s",
			data.Location, highTemp, unitSymbol, tempLow, tempHigh, unitSymbol, outcome)

	case "temperature_above":
		if highTemp >= data.Threshold {
			outcome = "Yes"
		} else {
			outcome = "No"
		}

	case "temperature_below":
		if highTemp <= data.Threshold {
			outcome = "Yes"
		} else {
			outcome = "No"
		}

	case "rain":
		rainTotal := 0.0
		if len(meteoResp.Daily.RainSum) > 0 {
			rainTotal = meteoResp.Daily.RainSum[0]
		}
		if len(meteoResp.Daily.PrecipSum) > 0 && rainTotal == 0 {
			rainTotal = meteoResp.Daily.PrecipSum[0]
		}
		if rainTotal > 0 {
			outcome = "Yes"
		} else {
			outcome = "No"
		}

	default:
		return nil, 0, fmt.Errorf("unknown weather condition: %s", data.Condition)
	}

	// Cache the result
	w.cache.Store(cacheKey, CachedResult{
		Outcome:    outcome,
		Confidence: confidence,
	})

	return &outcome, confidence, nil
}

// Helper function to parse date strings
func parseDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(strings.TrimSuffix(dateStr, "?"))

	// Try various date formats
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
			// If no year was parsed (year = 0), assume current year
			if t.Year() == 0 {
				now := time.Now()
				t = time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
				// If the date is more than 6 months in the future, it's probably last year
				if t.After(now.AddDate(0, 6, 0)) {
					t = t.AddDate(-1, 0, 0)
				}
			}
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse date: %s", dateStr)
}
