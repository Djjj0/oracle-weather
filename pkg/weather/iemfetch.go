package weather

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var iemHTTPClient = &http.Client{Timeout: 20 * time.Second}

// FetchDailyPeak fetches hourly METAR observations from IEM ASOS for a single day
// and returns the local hour (float64) and temperature in Celsius when the daily
// max was reached. Returns an error if no data is available or the fetch fails.
func FetchDailyPeak(station string, date time.Time, loc *time.Location) (peakHour float64, peakTempC float64, err error) {
	utcDate := date.UTC()
	tzName := loc.String()

	url := fmt.Sprintf(
		"https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py"+
			"?station=%s&data=tmpf,report_type&year1=%d&month1=%d&day1=%d"+
			"&year2=%d&month2=%d&day2=%d&tz=%s&format=onlycomma"+
			"&latlon=no&elev=no&missing=M&trace=T&direct=no&report_type=3&report_type=4",
		station,
		utcDate.Year(), int(utcDate.Month()), utcDate.Day(),
		utcDate.Year(), int(utcDate.Month()), utcDate.Day(),
		tzName,
	)

	var body []byte
	for attempt := 1; attempt <= 3; attempt++ {
		resp, httpErr := iemHTTPClient.Get(url)
		if httpErr != nil {
			if attempt == 3 {
				return 0, 0, httpErr
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
			return 0, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}

	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) < 2 {
		return 0, 0, fmt.Errorf("no data")
	}

	// CSV columns: station, valid (local time), tmpf[, report_type]
	peakTemp := -999.0
	for _, line := range lines[1:] {
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		// Drop SPECI only if report_type column is present
		if len(parts) >= 4 && strings.TrimSpace(parts[3]) == "4" {
			continue
		}
		tempStr := strings.TrimSpace(parts[2])
		if tempStr == "" || tempStr == "M" || tempStr == "T" {
			continue
		}
		temp, parseErr := strconv.ParseFloat(tempStr, 64)
		if parseErr != nil || temp < -100 || temp > 150 {
			continue
		}
		if temp > peakTemp {
			peakTemp = temp
			validStr := strings.TrimSpace(parts[1])
			t, parseErr := time.ParseInLocation("2006-01-02 15:04", validStr, loc)
			if parseErr == nil {
				peakHour = float64(t.Hour()) + float64(t.Minute())/60.0
			}
		}
	}

	if peakTemp == -999 {
		return 0, 0, fmt.Errorf("no valid readings")
	}

	// Convert °F to °C — learning DB stores temperatures in Celsius
	peakTempC = (peakTemp - 32) * 5 / 9
	return peakHour, peakTempC, nil
}
