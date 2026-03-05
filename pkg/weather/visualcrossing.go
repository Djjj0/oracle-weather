package weather

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

// VCClient handles Visual Crossing Weather API
type VCClient struct {
	apiKey string
	client *resty.Client
}

// VCDayData represents a day's weather data from Visual Crossing
type VCDayData struct {
	Datetime    string       `json:"datetime"`
	TempMax     float64      `json:"tempmax"`
	TempMin     float64      `json:"tempmin"`
	Temp        float64      `json:"temp"`
	Hours       []VCHourData `json:"hours"`
	Description string       `json:"description"`
}

// VCHourData represents hourly weather observations
type VCHourData struct {
	Datetime    string  `json:"datetime"`
	Temp        float64 `json:"temp"`
	Humidity    float64 `json:"humidity"`
	Precip      float64 `json:"precip"`
	Snow        float64 `json:"snow"`
	WindSpeed   float64 `json:"windspeed"`
	Pressure    float64 `json:"pressure"`
	Conditions  string  `json:"conditions"`
	DewPoint    float64 `json:"dew"`
	CloudCover  float64 `json:"cloudcover"`
}

// VCResponse represents the full API response
type VCResponse struct {
	Days []VCDayData `json:"days"`
}

// NewVCClient creates a new Visual Crossing client
func NewVCClient(apiKey string) *VCClient {
	client := resty.New().SetTimeout(30 * time.Second)
	client.SetHeader("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	return &VCClient{
		apiKey: apiKey,
		client: client,
	}
}

// FetchDayData fetches hourly data for a specific location and date
// location can be: "London,UK" or "51.5074,-0.1278" (lat,lon)
// date: the date to fetch data for
func (vc *VCClient) FetchDayData(location string, date time.Time) (*VCDayData, error) {
	// Visual Crossing Timeline API
	// Format: https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline/LOCATION/DATE?key=YOUR_API_KEY&include=hours

	dateStr := date.Format("2006-01-02")
	url := fmt.Sprintf(
		"https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline/%s/%s",
		location, dateStr,
	)

	resp, err := vc.client.R().
		SetQueryParams(map[string]string{
			"key":             vc.apiKey,
			"include":         "hours",
			"unitGroup":       "us",     // Use Fahrenheit
			"elements":        "datetime,temp,tempmax,tempmin,humidity,precip,snow,windspeed,pressure,dew,cloudcover,conditions",
			"contentType":     "json",
		}).
		Get(url)

	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var vcResp VCResponse
	if err := json.Unmarshal(resp.Body(), &vcResp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if len(vcResp.Days) == 0 {
		return nil, fmt.Errorf("no data returned for date %s", dateStr)
	}

	return &vcResp.Days[0], nil
}

// GetHighTemp extracts the highest temperature and when it occurred
func (vc *VCClient) GetHighTemp(location string, date time.Time, timezone string) (float64, time.Time, error) {
	dayData, err := vc.FetchDayData(location, date)
	if err != nil {
		return 0, time.Time{}, err
	}

	if len(dayData.Hours) == 0 {
		return 0, time.Time{}, fmt.Errorf("no hourly data available")
	}

	// Load timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("invalid timezone: %w", err)
	}

	// Find highest temperature
	var highTemp float64 = -999
	var highTempTime time.Time

	for _, hour := range dayData.Hours {
		if hour.Temp > highTemp {
			highTemp = hour.Temp

			// Parse time (format: "HH:MM:SS")
			hourTime, err := time.ParseInLocation("2006-01-02 15:04:05",
				fmt.Sprintf("%s %s", dayData.Datetime, hour.Datetime), loc)
			if err != nil {
				// If parsing fails, use the date with hour extracted from datetime string
				continue
			}
			highTempTime = hourTime
		}
	}

	if highTemp == -999 {
		return 0, time.Time{}, fmt.Errorf("no valid temperature found")
	}

	// Convert Fahrenheit to Celsius
	highTempC := (highTemp - 32) * 5 / 9

	return highTempC, highTempTime, nil
}

// GetDailyHighLow returns the high and low temps for a day (from daily summary, not hourly max/min)
func (vc *VCClient) GetDailyHighLow(location string, date time.Time) (float64, float64, error) {
	dayData, err := vc.FetchDayData(location, date)
	if err != nil {
		return 0, 0, err
	}

	// Convert F to C
	highC := (dayData.TempMax - 32) * 5 / 9
	lowC := (dayData.TempMin - 32) * 5 / 9

	return highC, lowC, nil
}
