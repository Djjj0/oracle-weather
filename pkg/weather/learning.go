package weather

import (
	"database/sql"
	"fmt"
	"sort"
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
	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=10000")
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

// DeleteCityData removes all records for a city from both market_history and city_stats.
func (l *LearningDB) DeleteCityData(city string) error {
	cityLC := strings.ToLower(city)
	if _, err := l.db.Exec(`DELETE FROM market_history WHERE city = ?`, cityLC); err != nil {
		return fmt.Errorf("delete market_history for %s: %w", city, err)
	}
	if _, err := l.db.Exec(`DELETE FROM city_stats WHERE city = ?`, cityLC); err != nil {
		return fmt.Errorf("delete city_stats for %s: %w", city, err)
	}
	return nil
}

// PurgeAnomalousRows deletes market_history rows whose high_temp_time, when
// converted to the row's stored IANA timezone, falls outside the daytime peak
// window [09:00, 22:00). These rows are cold-front midnight "peaks" or morning
// data-outage peaks that poison the learner (Chicago Apr 18 00:51 CDT). Returns
// the count of rows deleted and the list of distinct cities affected (for
// follow-up UpdateCityStats calls).
//
// Implementation note: SQLite's strftime() does not understand IANA tz names,
// so the filter is applied in Go after loading ids + timestamps.
func (l *LearningDB) PurgeAnomalousRows() (int64, []string, error) {
	rows, err := l.db.Query(`SELECT id, city, high_temp_time, timezone, high_temp FROM market_history`)
	if err != nil {
		return 0, nil, fmt.Errorf("scan market_history: %w", err)
	}
	defer rows.Close()

	var badIDs []int64
	citySet := map[string]struct{}{}
	for rows.Next() {
		var id int64
		var city, tz string
		var htt time.Time
		var highTemp float64
		if err := rows.Scan(&id, &city, &htt, &tz, &highTemp); err != nil {
			continue
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
		localHour := htt.In(loc).Hour()
		// Purge if: hour is outside daytime window OR high_temp is zero
		// (the latter is recordLearning legacy pollution — a position-close
		// row where entry time was written as if it were a peak observation).
		if localHour < 9 || localHour >= 22 || highTemp == 0 {
			badIDs = append(badIDs, id)
			citySet[city] = struct{}{}
		}
	}
	rows.Close()

	if len(badIDs) == 0 {
		return 0, nil, nil
	}

	tx, err := l.db.Begin()
	if err != nil {
		return 0, nil, fmt.Errorf("begin tx: %w", err)
	}
	stmt, err := tx.Prepare(`DELETE FROM market_history WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return 0, nil, fmt.Errorf("prepare: %w", err)
	}
	var deleted int64
	for _, id := range badIDs {
		res, err := stmt.Exec(id)
		if err != nil {
			stmt.Close()
			tx.Rollback()
			return deleted, nil, fmt.Errorf("delete id=%d: %w", id, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			deleted += n
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		return deleted, nil, fmt.Errorf("commit: %w", err)
	}

	cities := make([]string, 0, len(citySet))
	for c := range citySet {
		cities = append(cities, c)
	}
	return deleted, cities, nil
}

// DedupeMarketHistory collapses rows that share the same (market_id, DATE(date))
// down to one — keeping the row with the lowest id. Fixes the legacy triple-
// write pattern where nanosecond-precision on the `date` column bypassed the
// UNIQUE(market_id, date) constraint on concurrent on-demand fetches.
// Returns rows deleted and cities affected.
func (l *LearningDB) DedupeMarketHistory() (int64, []string, error) {
	selectCities := `
		SELECT DISTINCT city FROM market_history
		WHERE id NOT IN (
			SELECT MIN(id) FROM market_history GROUP BY market_id, DATE(date)
		)
	`
	rows, err := l.db.Query(selectCities)
	if err != nil {
		return 0, nil, fmt.Errorf("select affected cities: %w", err)
	}
	var cities []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err == nil {
			cities = append(cities, c)
		}
	}
	rows.Close()

	res, err := l.db.Exec(`
		DELETE FROM market_history
		WHERE id NOT IN (
			SELECT MIN(id) FROM market_history GROUP BY market_id, DATE(date)
		)
	`)
	if err != nil {
		return 0, cities, fmt.Errorf("dedupe market_history: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, cities, nil
}

// AddMarketPattern adds a historical market pattern.
//
// Uses INSERT OR IGNORE so the UNIQUE(market_id, date) constraint actually
// suppresses duplicates. Prior INSERT OR REPLACE behavior silently allowed
// the same (market_id, date) to be written 3× per call in fetchOnDemandPeakHour
// (see IDs 210/211/212 in learning.db — three rows for the same ondemand key),
// which triples the weight of every day and amplifies any stray outlier 3×.
func (l *LearningDB) AddMarketPattern(pattern MarketPattern) error {
	query := `
		INSERT OR IGNORE INTO market_history
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

// medianSorted returns the median of vs. Sorts vs in place. Returns 0 if empty.
func medianSorted(vs []float64) float64 {
	n := len(vs)
	if n == 0 {
		return 0
	}
	sort.Float64s(vs)
	if n%2 == 1 {
		return vs[n/2]
	}
	return (vs[n/2-1] + vs[n/2]) / 2.0
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

	var highHours, iemHours, resolutionHours []float64
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

		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}

		localHigh := float64(highTempTime.In(loc).Hour()) + float64(highTempTime.In(loc).Minute())/60.0
		localIEM := float64(iemFinalTime.In(loc).Hour()) + float64(iemFinalTime.In(loc).Minute())/60.0
		localResolved := float64(marketResolvedTime.In(loc).Hour()) + float64(marketResolvedTime.In(loc).Minute())/60.0

		// Skip rows whose recorded "high" timestamp is outside the daytime peak
		// window. These are cold-front midnight "peaks" or data-outage morning
		// peaks that poison the learner (Chicago Apr 2026 incident: one 00:51
		// CDT and one 10:50 CDT row pulled avg_high_temp_hour to 6.7, causing
		// pastPeak to fire at 09:29 CDT and lose $21.06 on a 98%-conf NO-inverse
		// bet). The sanity window matches peakHourMinLocal/peakHourMaxLocal in
		// weather_iem.go and is enforced here so legacy bad rows cannot reach
		// the aggregated stats even if they remain in market_history.
		if localHigh < 9.0 || localHigh >= 22.0 {
			continue
		}

		highHours = append(highHours, localHigh)
		iemHours = append(iemHours, localIEM)
		resolutionHours = append(resolutionHours, localResolved)

		if success {
			successCount++
		}
		timezone = tz
	}

	total := len(highHours)
	if total == 0 {
		return fmt.Errorf("no data found for city: %s", city)
	}

	// Use median, not mean — robust to residual outliers (data-outage days,
	// station errors). One poisoned row no longer determines the stat.
	avgHighHour := medianSorted(highHours)
	avgIEMHour := medianSorted(iemHours)
	avgResolutionHour := medianSorted(resolutionHours)
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

// HistoryCitySummary summarises one city's rows in market_history joined with city_stats.
type HistoryCitySummary struct {
	City        string
	Count       int
	MinDate     string
	MaxDate     string
	AvgPeakHour float64 // from city_stats (avg_high_temp_hour)
	Confidence  float64 // from city_stats (confidence_score)
}

// GetHistoryCitySummaries returns per-city row counts, date ranges, and learning stats.
func (l *LearningDB) GetHistoryCitySummaries() ([]HistoryCitySummary, error) {
	query := `
		SELECT h.city, COUNT(*), MIN(h.date), MAX(h.date),
		       COALESCE(cs.avg_high_temp_hour, 0),
		       COALESCE(cs.confidence_score, 0)
		FROM market_history h
		LEFT JOIN city_stats cs ON lower(h.city) = lower(cs.city)
		GROUP BY h.city
		ORDER BY h.city
	`
	rows, err := l.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []HistoryCitySummary
	for rows.Next() {
		var s HistoryCitySummary
		if err := rows.Scan(&s.City, &s.Count, &s.MinDate, &s.MaxDate, &s.AvgPeakHour, &s.Confidence); err != nil {
			continue
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// GetCityMarketHistory returns all market_history rows for a city ordered by date desc.
func (l *LearningDB) GetCityMarketHistory(city string) ([]MarketPattern, error) {
	query := `SELECT market_id, city, date, timezone, high_temp, high_temp_time,
	                 iem_data_final_time, market_resolved_time, optimal_entry_time,
	                 data_lag_minutes, resolution_lag_minutes, success, notes
	          FROM market_history WHERE lower(city) = lower(?) ORDER BY date DESC`
	rows, err := l.db.Query(query, strings.ToLower(city))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []MarketPattern
	for rows.Next() {
		var p MarketPattern
		if err := rows.Scan(
			&p.MarketID, &p.City, &p.Date, &p.Timezone, &p.HighTemp,
			&p.HighTempTime, &p.IEMDataFinalTime, &p.MarketResolvedTime,
			&p.OptimalEntryTime, &p.DataLagMinutes, &p.ResolutionLagMinutes,
			&p.Success, &p.Notes,
		); err != nil {
			continue
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
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
