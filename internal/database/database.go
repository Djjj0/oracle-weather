package database

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	tradesBucket        = []byte("trades")
	opportunitiesBucket = []byte("opportunities")
)

// Trade represents a completed trade
type Trade struct {
	ID             uint64    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	MarketID       string    `json:"market_id"`
	MarketQuestion string    `json:"market_question"`
	Outcome        string    `json:"outcome"`
	EntryPrice     float64   `json:"entry_price"`
	ExitPrice      float64   `json:"exit_price"`
	Profit         float64   `json:"profit"`
	Status         string    `json:"status"`
}

// Opportunity represents a potential trading opportunity
type Opportunity struct {
	ID             uint64    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	MarketID       string    `json:"market_id"`
	Outcome        string    `json:"outcome"`
	CurrentPrice   float64   `json:"current_price"`
	ExpectedProfit float64   `json:"expected_profit"`
	Executed       bool      `json:"executed"`
}

// DailyStats represents daily trading statistics
type DailyStats struct {
	Date                  string  `json:"date"`
	TotalTrades           int     `json:"total_trades"`
	SuccessfulTrades      int     `json:"successful_trades"`
	TotalProfit           float64 `json:"total_profit"`
	AverageProfit         float64 `json:"average_profit"`
	OpportunitiesFound    int     `json:"opportunities_found"`
	OpportunitiesExecuted int     `json:"opportunities_executed"`
}

// InitDB initializes the BoltDB database
func InitDB(dbPath string) (*bolt.DB, error) {
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create buckets
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(tradesBucket); err != nil {
			return fmt.Errorf("failed to create trades bucket: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(opportunitiesBucket); err != nil {
			return fmt.Errorf("failed to create opportunities bucket: %w", err)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return db, nil
}

// LogTrade inserts a trade record
func LogTrade(db *bolt.DB, trade Trade) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tradesBucket)

		// Generate ID
		id, _ := b.NextSequence()
		trade.ID = id
		trade.Timestamp = time.Now()

		// Encode trade
		encoded, err := json.Marshal(trade)
		if err != nil {
			return fmt.Errorf("failed to encode trade: %w", err)
		}

		// Store trade
		return b.Put(itob(id), encoded)
	})
}

// LogOpportunity inserts an opportunity record
func LogOpportunity(db *bolt.DB, opp Opportunity) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(opportunitiesBucket)

		// Generate ID
		id, _ := b.NextSequence()
		opp.ID = id
		opp.Timestamp = time.Now()

		// Encode opportunity
		encoded, err := json.Marshal(opp)
		if err != nil {
			return fmt.Errorf("failed to encode opportunity: %w", err)
		}

		// Store opportunity
		return b.Put(itob(id), encoded)
	})
}

// GetDailyStats retrieves daily statistics
func GetDailyStats(db *bolt.DB) (*DailyStats, error) {
	today := time.Now().Format("2006-01-02")

	stats := &DailyStats{
		Date: today,
	}

	err := db.View(func(tx *bolt.Tx) error {
		// Process trades
		tradesBkt := tx.Bucket(tradesBucket)
		c := tradesBkt.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var trade Trade
			if err := json.Unmarshal(v, &trade); err != nil {
				continue
			}

			// Check if trade is from today
			if trade.Timestamp.Format("2006-01-02") == today {
				stats.TotalTrades++
				if trade.Profit > 0 {
					stats.SuccessfulTrades++
				}
				stats.TotalProfit += trade.Profit
			}
		}

		// Calculate average profit
		if stats.TotalTrades > 0 {
			stats.AverageProfit = stats.TotalProfit / float64(stats.TotalTrades)
		}

		// Process opportunities
		oppBkt := tx.Bucket(opportunitiesBucket)
		c = oppBkt.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var opp Opportunity
			if err := json.Unmarshal(v, &opp); err != nil {
				continue
			}

			// Check if opportunity is from today
			if opp.Timestamp.Format("2006-01-02") == today {
				stats.OpportunitiesFound++
				if opp.Executed {
					stats.OpportunitiesExecuted++
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return stats, nil
}

// GetRecentOpportunities retrieves recent opportunities
func GetRecentOpportunities(db *bolt.DB, limit int) ([]Opportunity, error) {
	var opportunities []Opportunity

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(opportunitiesBucket)
		c := b.Cursor()

		// Iterate in reverse order (newest first)
		count := 0
		for k, v := c.Last(); k != nil && count < limit; k, v = c.Prev() {
			var opp Opportunity
			if err := json.Unmarshal(v, &opp); err != nil {
				continue
			}
			opportunities = append(opportunities, opp)
			count++
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return opportunities, nil
}

// Helper function to convert uint64 to bytes
func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}
