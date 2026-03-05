package weather

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ClimateRecord represents a daily temperature record for a location
type ClimateRecord struct {
	City       string
	Month      int
	Day        int
	RecordHigh float64
	RecordLow  float64
	HighYear   int
	LowYear    int
}

// RecordsClient handles climate record queries from a local SQLite cache
type RecordsClient struct {
	db *sql.DB
}

// NewRecordsClient creates a new climate records client
func NewRecordsClient(dbPath string) (*RecordsClient, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open records database: %w", err)
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS climate_records (
		city TEXT NOT NULL,
		month INTEGER NOT NULL,
		day INTEGER NOT NULL,
		record_high REAL NOT NULL,
		record_low REAL NOT NULL,
		high_year INTEGER,
		low_year INTEGER,
		last_updated TEXT,
		PRIMARY KEY (city, month, day)
	);
	CREATE INDEX IF NOT EXISTS idx_city ON climate_records(city);
	`
	if _, err := db.Exec(createTableSQL); err != nil {
		return nil, fmt.Errorf("failed to create records table: %w", err)
	}

	return &RecordsClient{db: db}, nil
}

// Close closes the database connection
func (rc *RecordsClient) Close() error {
	return rc.db.Close()
}

// GetRecordForDate retrieves the climate record for a specific date and location from the local cache.
// Returns an error if the record is not cached — records must be seeded manually.
func (rc *RecordsClient) GetRecordForDate(city string, month, day int) (*ClimateRecord, error) {
	var record ClimateRecord
	var lastUpdated string

	err := rc.db.QueryRow(`
		SELECT city, month, day, record_high, record_low, high_year, low_year, last_updated
		FROM climate_records
		WHERE city = ? AND month = ? AND day = ?
	`, city, month, day).Scan(
		&record.City, &record.Month, &record.Day,
		&record.RecordHigh, &record.RecordLow,
		&record.HighYear, &record.LowYear,
		&lastUpdated,
	)
	if err != nil {
		return nil, fmt.Errorf("no climate record cached for %s %d/%d: %w", city, month, day, err)
	}

	return &record, nil
}

// UpsertRecord inserts or updates a climate record in the cache
func (rc *RecordsClient) UpsertRecord(record ClimateRecord) error {
	_, err := rc.db.Exec(`
		INSERT OR REPLACE INTO climate_records
		(city, month, day, record_high, record_low, high_year, low_year, last_updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, record.City, record.Month, record.Day,
		record.RecordHigh, record.RecordLow,
		record.HighYear, record.LowYear,
		time.Now().Format("2006-01-02"),
	)
	return err
}

// CheckRecordHigh checks if actualHigh met or exceeded the cached record high
func (rc *RecordsClient) CheckRecordHigh(city string, date time.Time, actualHigh float64) (bool, float64, error) {
	record, err := rc.GetRecordForDate(city, int(date.Month()), date.Day())
	if err != nil {
		return false, 0, err
	}
	broke := actualHigh >= (record.RecordHigh - 0.1)
	return broke, record.RecordHigh, nil
}

// CheckRecordLow checks if actualLow was at or below the cached record low
func (rc *RecordsClient) CheckRecordLow(city string, date time.Time, actualLow float64) (bool, float64, error) {
	record, err := rc.GetRecordForDate(city, int(date.Month()), date.Day())
	if err != nil {
		return false, 0, err
	}
	broke := actualLow <= (record.RecordLow + 0.1)
	return broke, record.RecordLow, nil
}
