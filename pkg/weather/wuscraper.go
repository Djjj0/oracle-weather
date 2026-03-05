package weather

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/tebeka/selenium"
)

// WUScraper handles scraping Weather Underground using Selenium
type WUScraper struct {
	chromeDriverPath string
	service          *selenium.Service
	servicePort      int
}

// WUScrapedData represents scraped data from WU
type WUScrapedData struct {
	Date         time.Time
	TempHighF    float64
	TempHighC    float64
	TempLowF     float64
	TempLowC     float64
	HighTempTime time.Time
	Observations []WUHourlyObs
}

// WUHourlyObs represents an hourly observation
type WUHourlyObs struct {
	Time      time.Time
	TempF     float64
	TempC     float64
	Humidity  float64
	Pressure  float64
	Condition string
}

// NewWUScraper creates a new WU scraper
// chromeDriverPath: path to chromedriver executable (e.g., "./bin/chromedriver")
func NewWUScraper(chromeDriverPath string) (*WUScraper, error) {
	return &WUScraper{
		chromeDriverPath: chromeDriverPath,
	}, nil
}

// Start initializes the Selenium service
func (ws *WUScraper) Start() error {
	opts := []selenium.ServiceOption{
		selenium.Output(nil), // Suppress ChromeDriver logs
	}

	// Use port 9515 (ChromeDriver default)
	port := 9515
	service, err := selenium.NewChromeDriverService(ws.chromeDriverPath, port, opts...)
	if err != nil {
		return fmt.Errorf("failed to start ChromeDriver service: %w", err)
	}

	ws.service = service
	ws.servicePort = port
	return nil
}

// Stop shuts down the Selenium service
func (ws *WUScraper) Stop() error {
	if ws.service != nil {
		return ws.service.Stop()
	}
	return nil
}

// ScrapeDay scrapes WU for a specific station and date
// station: ICAO code (e.g., "EGLL" for London Heathrow)
// date: the date to scrape
// timezone: timezone for the location (e.g., "Europe/London")
func (ws *WUScraper) ScrapeDay(station string, date time.Time, timezone string) (*WUScrapedData, error) {
	// Create WebDriver capabilities
	caps := selenium.Capabilities{
		"browserName": "chrome",
		"goog:chromeOptions": map[string]interface{}{
			"args": []string{
				"--headless",              // Run in background
				"--no-sandbox",            // Security - required in some environments
				"--disable-dev-shm-usage", // Overcome limited resource problems
				"--disable-gpu",           // Disable GPU hardware acceleration
				"--window-size=1920,1080", // Set window size
			},
		},
	}

	// Connect to WebDriver using the service port
	if ws.service == nil {
		return nil, fmt.Errorf("ChromeDriver service not started - call Start() first")
	}

	serviceURL := fmt.Sprintf("http://localhost:%d/wd/hub", ws.servicePort)
	wd, err := selenium.NewRemote(caps, serviceURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create WebDriver at %s: %w", serviceURL, err)
	}
	defer wd.Quit() // CRITICAL: Always close WebDriver

	// Construct WU URL
	url := fmt.Sprintf(
		"https://www.wunderground.com/history/daily/%s/date/%d/%d/%d",
		station, date.Year(), date.Month(), date.Day(),
	)

	// Navigate to URL
	if err := wd.Get(url); err != nil {
		return nil, fmt.Errorf("failed to navigate to URL: %w", err)
	}

	// Wait for page to load (give JavaScript time to execute)
	time.Sleep(5 * time.Second)

	// Find the history table container
	tableElements, err := wd.FindElements(selenium.ByCSSSelector, "lib-city-history-observation")
	if err != nil || len(tableElements) == 0 {
		return nil, fmt.Errorf("failed to find history table on page")
	}

	// Get page source for parsing
	pageSource, err := wd.PageSource()
	if err != nil {
		return nil, fmt.Errorf("failed to get page source: %w", err)
	}

	// Parse the data
	data, err := ws.parseWUHTML(pageSource, date, timezone)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WU data: %w", err)
	}

	return data, nil
}

// parseWUHTML extracts temperature data from WU HTML using goquery
func (ws *WUScraper) parseWUHTML(html string, date time.Time, timezone string) (*WUScrapedData, error) {
	data := &WUScrapedData{
		Date: date,
	}

	// Load timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}

	// Parse HTML with goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Find all wu-value spans
	// WU displays hourly data in spans with class "wu-value wu-value-to"
	var tempValues []float64

	doc.Find("span.wu-value.wu-value-to").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text == "" || text == "--" {
			return // Skip empty/missing values
		}

		// Try to parse as float
		if val, err := strconv.ParseFloat(text, 64); err == nil {
			// Filter reasonable temperature values (between -50°F and 150°F)
			if val >= -50 && val <= 150 {
				tempValues = append(tempValues, val)
			}
		}
	})

	if len(tempValues) == 0 {
		return nil, fmt.Errorf("no temperature values found in HTML")
	}

	// WU shows hourly temps - group them into hourly observations
	// The pattern repeats: temp, dewpoint, humidity, wind, windspeed, pressure, precip
	// We want every 7th value starting from index 0 (temperatures)

	var observations []WUHourlyObs
	hour := 0

	// Extract temperatures (every ~7-8 values, depending on data columns)
	// Simplified: take first 24-30 values as potential hourly temps
	maxTemps := 24
	if len(tempValues) > maxTemps {
		tempValues = tempValues[:maxTemps*2] // Take extra in case of data gaps
	}

	// Extract what look like hourly temperature values
	for i := 0; i < len(tempValues) && hour < 24; i++ {
		tempF := tempValues[i]

		// Skip obviously non-temperature values (like pressure ~30)
		if tempF < 20 || tempF > 120 {
			continue
		}

		tempC := (tempF - 32) * 5 / 9
		obsTime := time.Date(date.Year(), date.Month(), date.Day(), hour, 0, 0, 0, loc)

		observations = append(observations, WUHourlyObs{
			Time:  obsTime,
			TempF: tempF,
			TempC: tempC,
		})

		hour++
	}

	if len(observations) == 0 {
		return nil, fmt.Errorf("no valid observations extracted")
	}

	data.Observations = observations

	// Find highest and lowest temperatures
	var highTemp float64 = -999
	var lowTemp float64 = 999
	var highTempTime time.Time

	for _, obs := range observations {
		if obs.TempC > highTemp {
			highTemp = obs.TempC
			highTempTime = obs.Time
			data.TempHighC = obs.TempC
			data.TempHighF = obs.TempF
		}

		if obs.TempC < lowTemp {
			lowTemp = obs.TempC
			data.TempLowC = obs.TempC
			data.TempLowF = obs.TempF
		}
	}

	data.HighTempTime = highTempTime

	return data, nil
}

// extractNumber extracts a number from a string
func extractNumber(s string) string {
	var result strings.Builder
	foundDigit := false

	for _, ch := range s {
		if ch >= '0' && ch <= '9' || ch == '.' || (ch == '-' && !foundDigit) {
			result.WriteRune(ch)
			if ch >= '0' && ch <= '9' {
				foundDigit = true
			}
		} else if foundDigit {
			break
		}
	}

	return result.String()
}

// ScrapeWithRetry attempts to scrape with exponential backoff
func (ws *WUScraper) ScrapeWithRetry(station string, date time.Time, timezone string, maxAttempts int) (*WUScrapedData, error) {
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		data, err := ws.ScrapeDay(station, date, timezone)
		if err == nil {
			return data, nil
		}

		lastErr = err

		if attempt < maxAttempts {
			// Exponential backoff: 2^attempt seconds
			waitTime := time.Duration(1<<uint(attempt)) * time.Second
			time.Sleep(waitTime)
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", maxAttempts, lastErr)
}
