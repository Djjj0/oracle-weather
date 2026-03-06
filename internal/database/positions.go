package database

import (
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	positionsBucket = []byte("positions")
)

// Position represents an open position
type Position struct {
	ID             uint64    `json:"id"`
	MarketID       string    `json:"market_id"`
	TokenID        string    `json:"token_id"`       // Token ID for selling
	MarketQuestion string    `json:"market_question"`
	Outcome        string    `json:"outcome"` // "YES" or "NO"
	EntryPrice     float64   `json:"entry_price"`
	PositionSize   float64   `json:"position_size"` // Amount invested in $
	Shares         float64   `json:"shares"`        // Number of shares bought
	EntryTime      time.Time `json:"entry_time"`
	Status         string    `json:"status"` // "OPEN", "CLAIMED", "LOST"
	ExitPrice      float64   `json:"exit_price"`
	Profit         float64   `json:"profit"`
	ClaimedAt      time.Time `json:"claimed_at"`
}

// OpenPosition records a new open position
func OpenPosition(db *bolt.DB, pos Position) error {
	return db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(positionsBucket)
		if err != nil {
			return fmt.Errorf("failed to create positions bucket: %w", err)
		}

		// Generate ID
		id, _ := b.NextSequence()
		pos.ID = id
		pos.EntryTime = time.Now()
		pos.Status = "OPEN"

		// Calculate shares: positionSize / entryPrice
		if pos.EntryPrice > 0 {
			pos.Shares = pos.PositionSize / pos.EntryPrice
		}

		// Encode position
		encoded, err := json.Marshal(pos)
		if err != nil {
			return fmt.Errorf("failed to encode position: %w", err)
		}

		// Store position
		return b.Put(itob(id), encoded)
	})
}

// GetOpenPositions retrieves all open positions
func GetOpenPositions(db *bolt.DB) ([]Position, error) {
	var positions []Position

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(positionsBucket)
		if b == nil {
			return nil // No positions yet
		}

		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var pos Position
			if err := json.Unmarshal(v, &pos); err != nil {
				continue
			}

			if pos.Status == "OPEN" {
				positions = append(positions, pos)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return positions, nil
}

// GetPositionsByMarket retrieves positions for a specific market
func GetPositionsByMarket(db *bolt.DB, marketID string) ([]Position, error) {
	var positions []Position

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(positionsBucket)
		if b == nil {
			return nil
		}

		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var pos Position
			if err := json.Unmarshal(v, &pos); err != nil {
				continue
			}

			if pos.MarketID == marketID {
				positions = append(positions, pos)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return positions, nil
}

// ClaimPosition marks a position as claimed and records profit
func ClaimPosition(db *bolt.DB, positionID uint64, exitPrice float64) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(positionsBucket)
		if b == nil {
			return fmt.Errorf("positions bucket not found")
		}

		// Get position
		v := b.Get(itob(positionID))
		if v == nil {
			return fmt.Errorf("position not found: %d", positionID)
		}

		var pos Position
		if err := json.Unmarshal(v, &pos); err != nil {
			return fmt.Errorf("failed to decode position: %w", err)
		}

		// Update position
		pos.Status = "CLAIMED"
		pos.ExitPrice = exitPrice
		pos.ClaimedAt = time.Now()

		// Calculate actual profit
		// If we won: profit = (shares * exitPrice) - positionSize
		// exitPrice should be 1.0 if we won, 0.0 if we lost
		pos.Profit = (pos.Shares * exitPrice) - pos.PositionSize

		// Encode updated position
		encoded, err := json.Marshal(pos)
		if err != nil {
			return fmt.Errorf("failed to encode position: %w", err)
		}

		// Update in database
		return b.Put(itob(positionID), encoded)
	})
}

// MarkPositionLost marks a position as lost (outcome was wrong)
func MarkPositionLost(db *bolt.DB, positionID uint64) error {
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(positionsBucket)
		if b == nil {
			return fmt.Errorf("positions bucket not found")
		}

		// Get position
		v := b.Get(itob(positionID))
		if v == nil {
			return fmt.Errorf("position not found: %d", positionID)
		}

		var pos Position
		if err := json.Unmarshal(v, &pos); err != nil {
			return fmt.Errorf("failed to decode position: %w", err)
		}

		// Update position
		pos.Status = "LOST"
		pos.ExitPrice = 0.0
		pos.Profit = -pos.PositionSize // Lost all money invested
		pos.ClaimedAt = time.Now()

		// Encode updated position
		encoded, err := json.Marshal(pos)
		if err != nil {
			return fmt.Errorf("failed to encode position: %w", err)
		}

		// Update in database
		return b.Put(itob(positionID), encoded)
	})
}

// GetAllClosedPositions retrieves all positions with status CLAIMED or LOST
func GetAllClosedPositions(db *bolt.DB) ([]Position, error) {
	var positions []Position

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(positionsBucket)
		if b == nil {
			return nil // No positions yet
		}

		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			var pos Position
			if err := json.Unmarshal(v, &pos); err != nil {
				continue
			}

			if pos.Status == "CLAIMED" || pos.Status == "LOST" {
				positions = append(positions, pos)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return positions, nil
}

// GetTotalExposure calculates total $ exposure across all open positions
func GetTotalExposure(db *bolt.DB) (float64, error) {
	positions, err := GetOpenPositions(db)
	if err != nil {
		return 0, err
	}

	var total float64
	for _, pos := range positions {
		total += pos.PositionSize
	}

	return total, nil
}
