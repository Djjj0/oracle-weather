package weather

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
)

// WUClient handles Weather Underground data fetching
type WUClient struct {
	client *resty.Client
}

// WUObservation represents a single weather observation from WU
type WUObservation struct {
	Time  time.Time
	TempC float64
	TempF float64
}

// NewWUClient creates a new Weather Underground client
func NewWUClient() *WUClient {
	client := resty.New().SetTimeout(30 * time.Second)
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36")

	return &WUClient{
		client: client,
	}
}

// FetchDayData fetches hourly observations for a specific day from Weather Underground
// station: ICAO code (e.g., "EGLL" for London Heathrow)
// date: the date to fetch data for
func (wuc *WUClient) FetchDayData(station string, date time.Time) ([]WUObservation, error) {
	// Weather Underground URL format:
	// https://www.wunderground.com/history/daily/STATION/date/YYYY/M/D
	url := fmt.Sprintf(
		"https://www.wunderground.com/history/daily/%s/date/%d/%d/%d",
		station, date.Year(), date.Month(), date.Day(),
	)

	resp, err := wuc.client.R().Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch WU data: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("WU returned status %s", resp.Status())
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	var observations []WUObservation

	// Weather Underground's HTML structure (as of 2024):
	// Hourly data is in a table with class containing "history-table" or similar
	// Each row has time and temperature data

	// Try multiple selectors since WU's HTML structure may vary
	selectors := []string{
		"table.history-table tbody tr",
		"table[class*='history'] tbody tr",
		"div[class*='observation'] table tr",
		"lib-city-history-observation table tbody tr",
	}

	var rows *goquery.Selection
	for _, selector := range selectors {
		rows = doc.Find(selector)
		if rows.Length() > 0 {
			break
		}
	}

	if rows.Length() == 0 {
		return nil, fmt.Errorf("no hourly data table found in HTML")
	}

	// Parse each row
	rows.Each(func(i int, row *goquery.Selection) {
		// Extract time
		timeText := strings.TrimSpace(row.Find("td:first-child, th:first-child").Text())

		// Extract temperature (typically in 2nd or 3rd column)
		tempText := ""
		row.Find("td").Each(func(j int, cell *goquery.Selection) {
			text := strings.TrimSpace(cell.Text())
			// Look for temperature pattern (number with ° or just number)
			if strings.Contains(text, "°") || (len(text) > 0 && (text[0] >= '0' && text[0] <= '9' || text[0] == '-')) {
				if tempText == "" {
					tempText = text
				}
			}
		})

		if timeText != "" && tempText != "" {
			// Parse temperature
			tempF, err := parseTemperature(tempText)
			if err == nil {
				// Convert to Celsius
				tempC := (tempF - 32) * 5 / 9

				// Parse time (simplified - WU typically shows "12:00 AM", "1:00 PM", etc.)
				obsTime := parseWUTime(date, timeText)

				observations = append(observations, WUObservation{
					Time:  obsTime,
					TempC: tempC,
					TempF: tempF,
				})
			}
		}
	})

	if len(observations) == 0 {
		return nil, fmt.Errorf("no observations extracted from HTML")
	}

	return observations, nil
}

// GetHighTemp fetches the day's high temperature from Weather Underground
func (wuc *WUClient) GetHighTemp(station string, date time.Time) (float64, time.Time, error) {
	observations, err := wuc.FetchDayData(station, date)
	if err != nil {
		return 0, time.Time{}, err
	}

	if len(observations) == 0 {
		return 0, time.Time{}, fmt.Errorf("no observations available")
	}

	// Find highest temperature
	highTemp := -999.0
	var highTempTime time.Time

	for _, obs := range observations {
		if obs.TempC > highTemp {
			highTemp = obs.TempC
			highTempTime = obs.Time
		}
	}

	if highTemp == -999 {
		return 0, time.Time{}, fmt.Errorf("no valid temperature found")
	}

	return highTemp, highTempTime, nil
}

// parseTemperature extracts numeric temperature from text like "15°C", "59°F", "15", etc.
func parseTemperature(text string) (float64, error) {
	// Remove degree symbols and units
	text = strings.ReplaceAll(text, "°", "")
	text = strings.ReplaceAll(text, "C", "")
	text = strings.ReplaceAll(text, "F", "")
	text = strings.ReplaceAll(text, "°C", "")
	text = strings.ReplaceAll(text, "°F", "")
	text = strings.TrimSpace(text)

	// Parse the number
	temp, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse temperature: %w", err)
	}

	return temp, nil
}

// parseWUTime parses Weather Underground time format (e.g., "12:00 AM", "1:53 PM")
func parseWUTime(date time.Time, timeText string) time.Time {
	// Try to parse time like "12:00 AM" or "1:53 PM"
	t, err := time.Parse("3:04 PM", strings.TrimSpace(timeText))
	if err != nil {
		// If parsing fails, return noon as default
		return time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, date.Location())
	}

	// Combine with the date
	return time.Date(date.Year(), date.Month(), date.Day(), t.Hour(), t.Minute(), 0, 0, date.Location())
}
