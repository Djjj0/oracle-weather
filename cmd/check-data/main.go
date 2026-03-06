package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "./data/learning.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Check sample data
	query := `SELECT city, date, high_temp, high_temp_time, iem_data_final_time, market_resolved_time, optimal_entry_time FROM market_history LIMIT 5`

	rows, err := db.Query(query)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	fmt.Println("=== SAMPLE DATA FROM DATABASE ===\n")

	for rows.Next() {
		var city, date string
		var highTemp float64
		var highTempTime, iemFinal, marketResolved, optimalEntry interface{}

		err := rows.Scan(&city, &date, &highTemp, &highTempTime, &iemFinal, &marketResolved, &optimalEntry)
		if err != nil {
			log.Printf("Scan error: %v", err)
			continue
		}

		fmt.Printf("City: %s\n", city)
		fmt.Printf("Date: %s\n", date)
		fmt.Printf("High Temp: %.1f°F\n", highTemp)
		fmt.Printf("High Temp Time: %v (type: %T)\n", highTempTime, highTempTime)
		fmt.Printf("IEM Final Time: %v (type: %T)\n", iemFinal, iemFinal)
		fmt.Printf("Market Resolved: %v (type: %T)\n", marketResolved, marketResolved)
		fmt.Printf("Optimal Entry: %v (type: %T)\n\n", optimalEntry, optimalEntry)
	}

	// Check total count
	var count int
	db.QueryRow("SELECT COUNT(*) FROM market_history").Scan(&count)
	fmt.Printf("Total records in database: %d\n", count)
}
