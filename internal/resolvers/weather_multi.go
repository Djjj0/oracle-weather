package resolvers

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/djbro/polymarket-oracle-bot/internal/config"
	"github.com/djbro/polymarket-oracle-bot/pkg/polymarket"
	"github.com/djbro/polymarket-oracle-bot/pkg/utils"
	"github.com/go-resty/resty/v2"
)

// MultiSourceWeatherResolver validates weather data across multiple APIs
type MultiSourceWeatherResolver struct {
	BaseResolver
	config *config.Config
	client *resty.Client
	cache  sync.Map
}

// cityToAirport maps cities to ICAO codes for IEM ASOS data
// Note: This is shared with IEM resolver - defined in weather_iem.go

// WeatherSource represents a single weather API source
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

// OpenMeteoHistoricalResponse for Open-Meteo API
type OpenMeteoHistoricalResponse struct {
	Daily struct {
		Time    []string  `json:"time"`
		TempMax []float64 `json:"temperature_2m_max"`
	} `json:"daily"`
}

// VisualCrossingResponse for Visual Crossing API
type VisualCrossingResponse struct {
	Days []struct {
		TempMax float64 `json:"tempmax"`
	} `json:"days"`
}

// OpenWeatherMapResponse for OpenWeatherMap history API
type OpenWeatherMapResponse struct {
	Data []struct {
		Temp struct {
			Max float64 `json:"max"`
		} `json:"temp"`
	} `json:"data"`
}

// CheckResolution validates weather across multiple sources
func (w *MultiSourceWeatherResolver) CheckResolution(market polymarket.Market) (*string, float64, error) {
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

	// Get airport code for IEM (ONLY source - Polymarket's official data)
	airportCode, hasAirport := cityToAirport[strings.ToLower(data.Location)]

	// Query IEM only
	sources := make(chan WeatherSource, 1)
	var wg sync.WaitGroup

	// Determine temperature unit
	useCelsius := false
	if unit, ok := data.Extra["unit"]; ok && unit == "celsius" {
		useCelsius = true
	}
	tempUnit := "fahrenheit"
	if useCelsius {
		tempUnit = "celsius"
	}
	dateStr := data.Date.Format("2006-01-02")

	// ONLY Source: IEM ASOS (Polymarket's EXACT source - Wunderground uses this!)
	if !hasAirport {
		return nil, 0, fmt.Errorf("no airport mapping for city: %s (IEM required)", data.Location)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		temp, err := w.getIEMTemp(airportCode, data.Date, useCelsius)
		sources <- WeatherSource{Name: "IEM/Wunderground", Temperature: temp, Success: err == nil, Error: err}
	}()

	// Wait and close
	go func() {
		wg.Wait()
		close(sources)
	}()

	// Collect results
	var results []WeatherSource
	for source := range sources {
		results = append(results, source)
		if source.Success {
			utils.Logger.Debugf("Weather source %s: %s on %s = %.1f°%s",
				source.Name, data.Location, dateStr, source.Temperature,
				map[string]string{"fahrenheit": "F", "celsius": "C"}[tempUnit])
		} else {
			utils.Logger.Warnf("Weather source %s failed: %v", source.Name, source.Error)
		}
	}

	// Use ONLY IEM (Polymarket's official source) - NO BACKUPS!
	if len(results) == 0 || !results[0].Success {
		return nil, 0, fmt.Errorf("IEM/Wunderground API failed - cannot trade without official data")
	}

	iemTemp := results[0].Temperature

	utils.Logger.Infof("✅ IEM/Wunderground (Polymarket's OFFICIAL source): %s = %.1f°%s",
		data.Location, iemTemp,
		map[string]string{"fahrenheit": "F", "celsius": "C"}[tempUnit])

	// Determine outcome based on IEM temperature
	outcome, confidence := w.determineOutcome(data, iemTemp, tempUnit)

	// Cache result
	w.cache.Store(cacheKey, CachedResult{Outcome: outcome, Confidence: confidence})

	// Cache result
	w.cache.Store(cacheKey, CachedResult{Outcome: outcome, Confidence: confidence})

	return &outcome, confidence, nil
}

// getIEMTemp fetches temperature from IEM ASOS (matches Wunderground)
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
		return 0, fmt.Errorf("no valid temps")
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

// getOpenMeteoTemp fetches temperature from Open-Meteo
func (w *MultiSourceWeatherResolver) getOpenMeteoTemp(coords [2]float64, date, unit string) (float64, error) {
	var resp OpenMeteoHistoricalResponse
	_, err := w.client.R().
		SetResult(&resp).
		Get(fmt.Sprintf(
			"https://archive-api.open-meteo.com/v1/archive?latitude=%.4f&longitude=%.4f&start_date=%s&end_date=%s&daily=temperature_2m_max&temperature_unit=%s&timezone=UTC",
			coords[0], coords[1], date, date, unit,
		))

	if err != nil || len(resp.Daily.TempMax) == 0 {
		return 0, fmt.Errorf("Open-Meteo API error: %v", err)
	}

	return resp.Daily.TempMax[0], nil
}

// getVisualCrossingTemp fetches temperature from Visual Crossing
func (w *MultiSourceWeatherResolver) getVisualCrossingTemp(coords [2]float64, date, location, unit string) (float64, error) {
	var resp VisualCrossingResponse

	unitCode := "us" // Fahrenheit
	if unit == "celsius" {
		unitCode = "metric"
	}

	_, err := w.client.R().
		SetResult(&resp).
		Get(fmt.Sprintf(
			"https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline/%s/%s?unitGroup=%s&key=%s&include=days&elements=tempmax",
			location, date, unitCode, w.config.VisualCrossingAPIKey,
		))

	if err != nil || len(resp.Days) == 0 {
		return 0, fmt.Errorf("Visual Crossing API error: %v", err)
	}

	return resp.Days[0].TempMax, nil
}

// getOpenWeatherMapTemp fetches temperature from OpenWeatherMap
func (w *MultiSourceWeatherResolver) getOpenWeatherMapTemp(coords [2]float64, date, unit string) (float64, error) {
	// OpenWeatherMap uses Unix timestamp for historical data
	parsedDate, _ := time.Parse("2006-01-02", date)
	timestamp := parsedDate.Unix()

	var resp OpenWeatherMapResponse

	unitParam := "imperial" // Fahrenheit
	if unit == "celsius" {
		unitParam = "metric"
	}

	_, err := w.client.R().
		SetResult(&resp).
		Get(fmt.Sprintf(
			"https://api.openweathermap.org/data/2.5/onecall/timemachine?lat=%.4f&lon=%.4f&dt=%d&units=%s&appid=%s",
			coords[0], coords[1], timestamp, unitParam, w.config.OpenWeatherAPIKey,
		))

	if err != nil || len(resp.Data) == 0 {
		return 0, fmt.Errorf("OpenWeatherMap API error: %v", err)
	}

	return resp.Data[0].Temp.Max, nil
}

// determineOutcome determines market outcome based on temperature
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

// ParseMarketQuestion delegates to base weather resolver
func (w *MultiSourceWeatherResolver) ParseMarketQuestion(question string) (*MarketData, error) {
	// Use the existing weather resolver's parsing logic
	wr := NewWeatherResolver(w.config)
	return wr.ParseMarketQuestion(question)
}

// median calculates median of float64 slice
func median(numbers []float64) float64 {
	if len(numbers) == 0 {
		return 0
	}
	if len(numbers) == 1 {
		return numbers[0]
	}

	// Sort
	sorted := make([]float64, len(numbers))
	copy(sorted, numbers)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}
