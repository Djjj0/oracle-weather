package weather

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
)

// HurricaneCategory defines Saffir-Simpson categories
type HurricaneCategory int

const (
	TropicalStorm HurricaneCategory = 0  // 39-73 mph
	Category1     HurricaneCategory = 1  // 74-95 mph
	Category2     HurricaneCategory = 2  // 96-110 mph
	Category3     HurricaneCategory = 3  // 111-129 mph
	Category4     HurricaneCategory = 4  // 130-156 mph
	Category5     HurricaneCategory = 5  // 157+ mph
)

// HurricaneClient handles hurricane data queries
type HurricaneClient struct {
	client *resty.Client
	cache  sync.Map
}

// NHCStorm represents a storm from NOAA NHC
type NHCStorm struct {
	Name      string  `json:"name"`
	ID        string  `json:"id"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	WindSpeed float64 `json:"windSpeed"` // mph
	Category  int     `json:"category"`
	Status    string  `json:"status"` // "Hurricane", "Tropical Storm", etc.
}

// NHCResponse represents the CurrentStorms.json structure
type NHCResponse struct {
	ActiveStorms []NHCStorm `json:"activeStorms"`
}


// NewHurricaneClient creates a new hurricane data client
func NewHurricaneClient() *HurricaneClient {
	return &HurricaneClient{
		client: resty.New().SetTimeout(15 * time.Second),
	}
}

// GetCategoryFromWindSpeed returns hurricane category based on sustained wind speed (mph)
func GetCategoryFromWindSpeed(windSpeedMPH float64) HurricaneCategory {
	if windSpeedMPH >= 157 {
		return Category5
	} else if windSpeedMPH >= 130 {
		return Category4
	} else if windSpeedMPH >= 111 {
		return Category3
	} else if windSpeedMPH >= 96 {
		return Category2
	} else if windSpeedMPH >= 74 {
		return Category1
	} else if windSpeedMPH >= 39 {
		return TropicalStorm
	}
	return -1 // Not hurricane strength
}

// GetActiveStorms fetches current active storms from NOAA NHC
func (hc *HurricaneClient) GetActiveStorms() ([]NHCStorm, error) {
	// Check cache (refresh every 6 hours)
	cacheKey := "active_storms"
	if cached, ok := hc.cache.Load(cacheKey); ok {
		cachedData := cached.(struct {
			storms    []NHCStorm
			timestamp time.Time
		})
		if time.Since(cachedData.timestamp) < 6*time.Hour {
			return cachedData.storms, nil
		}
	}

	// Fetch from NOAA NHC
	var nhcResp NHCResponse
	resp, err := hc.client.R().
		SetResult(&nhcResp).
		Get("https://www.nhc.noaa.gov/CurrentStorms.json")

	if err != nil {
		return nil, fmt.Errorf("failed to fetch NHC data: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("NHC API returned error: %s", resp.Status())
	}

	// Cache result
	hc.cache.Store(cacheKey, struct {
		storms    []NHCStorm
		timestamp time.Time
	}{
		storms:    nhcResp.ActiveStorms,
		timestamp: time.Now(),
	})

	return nhcResp.ActiveStorms, nil
}

// CheckHurricaneLandfall checks if a hurricane made landfall at a specific location and category
// This is for historical verification using wind speed data
func (hc *HurricaneClient) CheckHurricaneLandfall(
	lat, lon float64,
	hurricaneName string,
	minCategory int,
	startDate, endDate time.Time,
) (bool, int, error) {
	// Build cache key
	cacheKey := fmt.Sprintf("hurricane_%s_%.4f_%.4f_%s_%s",
		strings.ToLower(hurricaneName),
		lat, lon,
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"),
	)

	// Check cache
	if cached, ok := hc.cache.Load(cacheKey); ok {
		result := cached.(struct {
			reached  bool
			category int
		})
		return result.reached, result.category, nil
	}

	// Historical wind verification is not supported without a data source.
	// Use NHC active storm data (GetActiveStorms) for current storms only.
	return false, 0, fmt.Errorf("historical hurricane wind verification not supported: use NHC active storm data for current events")
}

// GetHurricaneByName searches active storms for a specific hurricane
func (hc *HurricaneClient) GetHurricaneByName(name string) (*NHCStorm, error) {
	storms, err := hc.GetActiveStorms()
	if err != nil {
		return nil, err
	}

	nameLower := strings.ToLower(name)
	for _, storm := range storms {
		if strings.ToLower(storm.Name) == nameLower {
			return &storm, nil
		}
	}

	return nil, fmt.Errorf("hurricane %s not found in active storms", name)
}

// ParseNHCJSON parses a custom NHC JSON endpoint (if available)
// Some storms have individual JSON endpoints like:
// https://www.nhc.noaa.gov/storm_graphics/api/AL092023_TRACK_latest.json
func (hc *HurricaneClient) ParseNHCJSON(stormID string) (*NHCStorm, error) {
	// Try to fetch storm-specific data
	url := fmt.Sprintf("https://www.nhc.noaa.gov/storm_graphics/api/%s_TRACK_latest.json", stormID)

	resp, err := hc.client.R().Get(url)
	if err != nil || resp.IsError() {
		return nil, fmt.Errorf("failed to fetch storm data for %s", stormID)
	}

	// Parse the JSON response
	var stormData map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &stormData); err != nil {
		return nil, fmt.Errorf("failed to parse storm JSON: %w", err)
	}

	// Extract relevant fields (structure varies by storm)
	storm := &NHCStorm{
		ID: stormID,
	}

	// Parse fields if they exist
	if name, ok := stormData["name"].(string); ok {
		storm.Name = name
	}
	if windSpeed, ok := stormData["windSpeed"].(float64); ok {
		storm.WindSpeed = windSpeed
		storm.Category = int(GetCategoryFromWindSpeed(windSpeed))
	}

	return storm, nil
}

// GetCategoryName returns the human-readable name for a category
func GetCategoryName(category HurricaneCategory) string {
	switch category {
	case TropicalStorm:
		return "Tropical Storm"
	case Category1:
		return "Category 1 Hurricane"
	case Category2:
		return "Category 2 Hurricane"
	case Category3:
		return "Category 3 Hurricane"
	case Category4:
		return "Category 4 Hurricane"
	case Category5:
		return "Category 5 Hurricane"
	default:
		return "Below Hurricane Strength"
	}
}
