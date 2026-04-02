package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var hkoHTTPClient = &http.Client{Timeout: 15 * time.Second}

// hkoDailyExtract is the JSON structure returned by the HKO Daily Extract endpoint.
// Despite the .xml extension, the endpoint returns JSON.
type hkoDailyExtract struct {
	Stn struct {
		Data []struct {
			Month   int               `json:"month"`
			DayData [][]interface{}   `json:"dayData"`
		} `json:"data"`
	} `json:"stn"`
}

// FetchHKODailyMax fetches the absolute daily maximum temperature (°C) from the
// Hong Kong Observatory Daily Extract for the given date.
// This matches Polymarket's resolution source for Hong Kong temperature markets.
// Returns (0, nil) if data for the requested day is not yet published.
func FetchHKODailyMax(date time.Time) (float64, error) {
	hkLoc, err := time.LoadLocation("Asia/Hong_Kong")
	if err != nil {
		return 0, fmt.Errorf("HKO: failed to load timezone: %w", err)
	}
	localDate := date.In(hkLoc)

	url := fmt.Sprintf(
		"https://www.weather.gov.hk/cis/dailyExtract/dailyExtract_%d%02d.xml",
		localDate.Year(), int(localDate.Month()),
	)

	var body []byte
	for attempt := 1; attempt <= 3; attempt++ {
		resp, httpErr := hkoHTTPClient.Get(url)
		if httpErr != nil {
			if attempt == 3 {
				return 0, httpErr
			}
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			break
		}
		if attempt == 3 {
			return 0, fmt.Errorf("HKO API HTTP %d", resp.StatusCode)
		}
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}

	var extract hkoDailyExtract
	if err := json.Unmarshal(body, &extract); err != nil {
		return 0, fmt.Errorf("HKO parse error: %w", err)
	}

	targetDay := fmt.Sprintf("%02d", localDate.Day())

	for _, monthData := range extract.Stn.Data {
		for _, dayRow := range monthData.DayData {
			if len(dayRow) < 3 {
				continue
			}
			// Field 0: day number (e.g. "01")
			dayStr, ok := dayRow[0].(string)
			if !ok {
				continue
			}
			if strings.TrimSpace(dayStr) != targetDay {
				continue
			}
			// Field 2: absolute daily max temperature in °C
			switch v := dayRow[2].(type) {
			case float64:
				return v, nil
			case string:
				s := strings.TrimSpace(v)
				if s == "" || s == "---" || s == "N/A" {
					// Day exists but max temp not yet finalised
					return 0, nil
				}
				temp, parseErr := strconv.ParseFloat(s, 64)
				if parseErr != nil {
					return 0, fmt.Errorf("HKO temp parse error for day %s: %w", targetDay, parseErr)
				}
				return temp, nil
			}
		}
	}

	// Day not in extract yet — data not published for this date
	return 0, nil
}
