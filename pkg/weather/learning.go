package weather

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// MarketPattern represents historical market behavior
type MarketPattern struct {
	ID                   int64
	MarketID             string
	City                 string
	Date                 time.Time
	Timezone             string
	HighTemp             float64
	HighTempTime         time.Time    // When actual high was reached (from IEM)
	IEMDataFinalTime     time.Time    // When IEM stopped updating (last observation)
	MarketResolvedTime   time.Time    // When Polymarket resolved the market
	OptimalEntryTime     time.Time    // Calculated optimal entry
	DataLagMinutes       int          // Minutes between high temp and IEM final
	ResolutionLagMinutes int          // Minutes between IEM final and market resolution
	Success              bool         // Would we have caught this opportunity?
	Notes                string
}

// CityStats represents aggregated statistics for a city
type CityStats struct {
	City                       string
	TotalMarkets               int
	AvgHighTempHour            float64 // Average hour (0-23) when high is reached
	AvgIEMFinalHour            float64 // Average hour when IEM data is final
	AvgMarketResolutionHour    float64 // Average hour when market resolves
	OptimalEntryHour           float64 // Recommended entry hour
	SuccessRate                float64 // % of opportunities we'd catch at optimal time
	ConfidenceScore            float64 // 0-1 based on data consistency
	Timezone                   string
	LastUpdated                time.Time
}

// LearningDB manages the learning database
type LearningDB struct {
	db *sql.DB
}

// NewLearningDB creates/opens the learning database
func NewLearningDB(dbPath string) (*LearningDB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open learning database: %w", err)
	}

	// Create tables
	createTablesSQL := `
	CREATE TABLE IF NOT EXISTS market_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		market_id TEXT NOT NULL,
		city TEXT NOT NULL,
		date DATE NOT NULL,
		timezone TEXT,
		high_temp REAL,
		high_temp_time TIMESTAMP,
		iem_data_final_time TIMESTAMP,
		market_resolved_time TIMESTAMP,
		optimal_entry_time TIMESTAMP,
		data_lag_minutes INTEGER,
		resolution_lag_minutes INTEGER,
		success BOOLEAN,
		notes TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(market_id, date)
	);

	CREATE INDEX IF NOT EXISTS idx_city_date ON market_history(city, date);
	CREATE INDEX IF NOT EXISTS idx_date ON market_history(date);

	CREATE TABLE IF NOT EXISTS city_stats (
		city TEXT PRIMARY KEY,
		total_markets INTEGER,
		avg_high_temp_hour REAL,
		avg_iem_final_hour REAL,
		avg_market_resolution_hour REAL,
		optimal_entry_hour REAL,
		success_rate REAL,
		confidence_score REAL,
		timezone TEXT,
		last_updated TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS city_timezone_map (
		city TEXT PRIMARY KEY,
		iem_station TEXT,
		timezone TEXT,
		lat REAL,
		lon REAL
	);

	-- Pre-populate city timezone mappings
	INSERT OR IGNORE INTO city_timezone_map (city, iem_station, timezone, lat, lon) VALUES
		('seattle', 'KSEA', 'America/Los_Angeles', 47.6062, -122.3321),
		('chicago', 'KORD', 'America/Chicago', 41.8781, -87.6298),
		('new york', 'KJFK', 'America/New_York', 40.7128, -74.0060),
		('miami', 'KMIA', 'America/New_York', 25.7617, -80.1918),
		('phoenix', 'KPHX', 'America/Phoenix', 33.4484, -112.0740),
		('los angeles', 'KLAX', 'America/Los_Angeles', 34.0522, -118.2437),
		('denver', 'KDEN', 'America/Denver', 39.7392, -104.9903),
		('dallas', 'KDFW', 'America/Chicago', 32.7767, -96.7970),
		('houston', 'KIAH', 'America/Chicago', 29.7604, -95.3698),
		('atlanta', 'KATL', 'America/New_York', 33.7490, -84.3880),
		('boston', 'KBOS', 'America/New_York', 42.3601, -71.0589),
		('san francisco', 'KSFO', 'America/Los_Angeles', 37.7749, -122.4194);
	`

	// WAL mode: allows concurrent reads while writing — eliminates SQLITE_BUSY
	// under the multi-goroutine scan workload.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}
	// Give blocked writers up to 5 seconds before returning SQLITE_BUSY.
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	if _, err := db.Exec(createTablesSQL); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return &LearningDB{db: db}, nil
}

// Close closes the database connection
func (l *LearningDB) Close() error {
	return l.db.Close()
}

// AddMarketPattern adds a historical market pattern
func (l *LearningDB) AddMarketPattern(pattern MarketPattern) error {
	query := `
		INSERT OR REPLACE INTO market_history
		(market_id, city, date, timezone, high_temp, high_temp_time,
		 iem_data_final_time, market_resolved_time, optimal_entry_time,
		 data_lag_minutes, resolution_lag_minutes, success, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := l.db.Exec(query,
		pattern.MarketID, pattern.City, pattern.Date, pattern.Timezone,
		pattern.HighTemp, pattern.HighTempTime, pattern.IEMDataFinalTime,
		pattern.MarketResolvedTime, pattern.OptimalEntryTime,
		pattern.DataLagMinutes, pattern.ResolutionLagMinutes,
		pattern.Success, pattern.Notes,
	)

	return err
}

// GetCityStats retrieves statistics for a city
func (l *LearningDB) GetCityStats(city string) (*CityStats, error) {
	query := `SELECT * FROM city_stats WHERE city = ?`

	var stats CityStats
	err := l.db.QueryRow(query, city).Scan(
		&stats.City, &stats.TotalMarkets,
		&stats.AvgHighTempHour, &stats.AvgIEMFinalHour,
		&stats.AvgMarketResolutionHour, &stats.OptimalEntryHour,
		&stats.SuccessRate, &stats.ConfidenceScore,
		&stats.Timezone, &stats.LastUpdated,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no stats for city: %s", city)
	}

	return &stats, err
}

// UpdateCityStats recalculates and updates statistics for a city
func (l *LearningDB) UpdateCityStats(city string) error {
	// Query to get raw timestamp data - we'll parse it in Go
	query := `
		SELECT high_temp_time, iem_data_final_time, market_resolved_time, success, timezone
		FROM market_history
		WHERE city = ?
		ORDER BY date DESC
	`

	rows, err := l.db.Query(query, city)
	if err != nil {
		return fmt.Errorf("failed to query data: %w", err)
	}
	defer rows.Close()

	var total int
	var sumHighHour, sumIEMHour, sumResolutionHour float64
	var successCount int
	var timezone string

	for rows.Next() {
		var highTempTime, iemFinalTime, marketResolvedTime time.Time
		var success bool
		var tz string

		err := rows.Scan(&highTempTime, &iemFinalTime, &marketResolvedTime, &success, &tz)
		if err != nil {
			continue
		}

		// Load timezone location for conversion
		loc, err := time.LoadLocation(tz)
		if err != nil {
			// Fall back to UTC if timezone load fails
			loc = time.UTC
		}

		// Convert timestamps to local timezone BEFORE extracting hour
		localHighTime := highTempTime.In(loc)
		localIEMTime := iemFinalTime.In(loc)
		localResolvedTime := marketResolvedTime.In(loc)

		// Extract hour as float (hour + minutes/60) from LOCAL time
		sumHighHour += float64(localHighTime.Hour()) + float64(localHighTime.Minute())/60.0
		sumIEMHour += float64(localIEMTime.Hour()) + float64(localIEMTime.Minute())/60.0
		sumResolutionHour += float64(localResolvedTime.Hour()) + float64(localResolvedTime.Minute())/60.0

		if success {
			successCount++
		}

		total++
		timezone = tz
	}

	if total == 0 {
		return fmt.Errorf("no data found for city: %s", city)
	}

	// Calculate averages
	avgHighHour := sumHighHour / float64(total)
	avgIEMHour := sumIEMHour / float64(total)
	avgResolutionHour := sumResolutionHour / float64(total)
	successRate := float64(successCount) / float64(total)

	// Calculate optimal entry time
	// Use high temp time + 1 hour safety buffer to ensure temp won't go higher
	// This puts us in the window BEFORE market resolves
	optimalEntryHour := avgHighHour + 1.0

	// Calculate confidence score based on data consistency
	confidenceScore := calculateConfidence(total, successRate)

	// Update city_stats table
	updateQuery := `
		INSERT OR REPLACE INTO city_stats
		(city, total_markets, avg_high_temp_hour, avg_iem_final_hour,
		 avg_market_resolution_hour, optimal_entry_hour, success_rate,
		 confidence_score, timezone, last_updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = l.db.Exec(updateQuery,
		city, total, avgHighHour, avgIEMHour, avgResolutionHour,
		optimalEntryHour, successRate, confidenceScore, timezone, time.Now(),
	)

	return err
}

// GetAllCityStats returns stats for all cities
func (l *LearningDB) GetAllCityStats() ([]CityStats, error) {
	query := `SELECT * FROM city_stats ORDER BY city`

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []CityStats
	for rows.Next() {
		var s CityStats
		err := rows.Scan(
			&s.City, &s.TotalMarkets,
			&s.AvgHighTempHour, &s.AvgIEMFinalHour,
			&s.AvgMarketResolutionHour, &s.OptimalEntryHour,
			&s.SuccessRate, &s.ConfidenceScore,
			&s.Timezone, &s.LastUpdated,
		)
		if err != nil {
			continue
		}
		stats = append(stats, s)
	}

	return stats, nil
}

// GetCityTimezone looks up timezone for a city
func (l *LearningDB) GetCityTimezone(city string) (string, string, error) {
	query := `SELECT iem_station, timezone FROM city_timezone_map WHERE city = ?`

	var station, tz string
	err := l.db.QueryRow(query, city).Scan(&station, &tz)
	if err != nil {
		return "", "", fmt.Errorf("city not found: %s", city)
	}

	return station, tz, nil
}

// calculateConfidence returns a confidence score (0-1) based on data quality
func calculateConfidence(totalMarkets int, successRate float64) float64 {
	// More data = higher confidence
	dataConfidence := float64(totalMarkets) / 100.0
	if dataConfidence > 1.0 {
		dataConfidence = 1.0
	}

	// Higher success rate = higher confidence
	rateConfidence := successRate

	// Combined confidence (weighted average)
	return (dataConfidence*0.4 + rateConfidence*0.6)
}

// GetOptimalEntryByStation looks up optimal entry time by weather station code
func (l *LearningDB) GetOptimalEntryByStation(stationCode string) (float64, string, error) {
	query := `
		SELECT cs.optimal_entry_hour, cs.timezone
		FROM city_timezone_map ctm
		JOIN city_stats cs ON ctm.city = cs.city
		WHERE ctm.iem_station = ?
	`

	var optimalHour float64
	var timezone string

	err := l.db.QueryRow(query, stationCode).Scan(&optimalHour, &timezone)
	if err != nil {
		return 0, "", fmt.Errorf("station not found: %s", stationCode)
	}

	return optimalHour, timezone, nil
}

// RecordMarketOutcome records the outcome of a resolved market and updates city stats.
// Called by the bot after a position closes so the learning DB stays current.
// highTempTime is the local hour when IEM first showed the confirmed high.
func (l *LearningDB) RecordMarketOutcome(marketID, city, timezone string, date time.Time, highTemp float64, highTempTime time.Time, entryTime time.Time, success bool) error {
	iemFinalTime := highTempTime.Add(30 * time.Minute) // conservative: IEM finalises ~30m after high
	optimalEntry := entryTime

	pattern := MarketPattern{
		MarketID:         marketID,
		City:             strings.ToLower(city),
		Date:             date,
		Timezone:         timezone,
		HighTemp:         highTemp,
		HighTempTime:     highTempTime,
		IEMDataFinalTime: iemFinalTime,
		MarketResolvedTime: time.Now(),
		OptimalEntryTime:   optimalEntry,
		DataLagMinutes:   int(iemFinalTime.Sub(highTempTime).Minutes()),
		ResolutionLagMinutes: int(time.Now().Sub(iemFinalTime).Minutes()),
		Success:          success,
	}

	if err := l.AddMarketPattern(pattern); err != nil {
		return fmt.Errorf("failed to add market pattern: %w", err)
	}

	// Recompute city_stats so OptimalEntryHour reflects the new data point
	if err := l.UpdateCityStats(strings.ToLower(city)); err != nil {
		// Non-fatal: we recorded the raw data, stats update can retry next time
		return fmt.Errorf("market pattern saved but city stats update failed: %w", err)
	}

	return nil
}

// GetCityByStation looks up city name by station code
func (l *LearningDB) GetCityByStation(stationCode string) (string, error) {
	query := `SELECT city FROM city_timezone_map WHERE iem_station = ?`

	var city string
	err := l.db.QueryRow(query, stationCode).Scan(&city)
	if err != nil {
		return "", fmt.Errorf("station not found: %s", stationCode)
	}

	return city, nil
}
