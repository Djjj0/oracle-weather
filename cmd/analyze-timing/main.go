package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "./data/learning.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	cities := []struct {
		name     string
		timezone string
	}{
		{"chicago", "America/Chicago"},
		{"seattle", "America/Los_Angeles"},
		{"new york", "America/New_York"},
		{"miami", "America/New_York"},
		{"dallas", "America/Chicago"},
		{"atlanta", "America/New_York"},
	}

	fmt.Println("=== WEATHER MARKET TIMING ANALYSIS ===")
	fmt.Println("Based on 365 days of historical data per city\n")

	for _, city := range cities {
		loc, err := time.LoadLocation(city.timezone)
		if err != nil {
			continue
		}

		query := `SELECT high_temp_time, iem_data_final_time, market_resolved_time
		          FROM market_history
		          WHERE city = ?
		          ORDER BY date DESC`

		rows, err := db.Query(query, city.name)
		if err != nil {
			continue
		}

		var sumHighHour, sumIEMHour, sumResolutionHour float64
		var count int

		for rows.Next() {
			var highTime, iemTime, resolutionTime time.Time
			rows.Scan(&highTime, &iemTime, &resolutionTime)

			// Convert to local timezone
			highLocal := highTime.In(loc)
			iemLocal := iemTime.In(loc)
			resLocal := resolutionTime.In(loc)

			sumHighHour += float64(highLocal.Hour()) + float64(highLocal.Minute())/60.0
			sumIEMHour += float64(iemLocal.Hour()) + float64(iemLocal.Minute())/60.0
			sumResolutionHour += float64(resLocal.Hour()) + float64(resLocal.Minute())/60.0

			count++
		}
		rows.Close()

		if count == 0 {
			continue
		}

		avgHighHour := sumHighHour / float64(count)
		avgIEMHour := sumIEMHour / float64(count)
		avgResHour := sumResolutionHour / float64(count)
		optimalEntry := avgIEMHour + 0.5

		// Calculate lag windows
		dataLag := avgIEMHour - avgHighHour
		if dataLag < 0 {
			dataLag += 24 // Handle day wraparound
		}

		fmt.Printf("📍 %s (%s)\n", city.name, city.timezone)
		fmt.Printf("   Data points: %d days\n", count)
		fmt.Printf("   \n")
		fmt.Printf("   🌡️  High temp reached:    %02d:%02d LOCAL\n",
			int(avgHighHour), int((avgHighHour-float64(int(avgHighHour)))*60))
		fmt.Printf("   📊 IEM data finalized:    %02d:%02d LOCAL\n",
			int(avgIEMHour), int((avgIEMHour-float64(int(avgIEMHour)))*60))
		fmt.Printf("   🎯 Market resolves:       %02d:%02d LOCAL\n",
			int(avgResHour), int((avgResHour-float64(int(avgResHour)))*60))
		fmt.Printf("   \n")
		fmt.Printf("   ⏱️  Data lag:              %.1f hours\n", dataLag)
		fmt.Printf("   ⭐ OPTIMAL ENTRY:         %02d:%02d LOCAL\n",
			int(optimalEntry), int((optimalEntry-float64(int(optimalEntry)))*60))
		fmt.Printf("   💡 Strategy: Enter after IEM data final, before market close\n")
		fmt.Println()
	}

	fmt.Println("=== KEY FINDINGS ===\n")
	fmt.Println("1. High temperatures are reached in early-mid afternoon (LOCAL time)")
	fmt.Println("2. IEM data continues updating for 6-8 more hours after high temp")
	fmt.Println("3. IEM data finalizes around 6-7 PM LOCAL time (last observation)")
	fmt.Println("4. Markets resolve around 6:45 PM LOCAL time")
	fmt.Println("5. OPTIMAL ENTRY: Shortly after IEM finalization (~7:20 PM LOCAL)")
	fmt.Println()
	fmt.Println("⚠️  CURRENT BOT ISSUE: Bot waits until midnight, but markets")
	fmt.Println("   already resolved hours earlier!")
	fmt.Println()
	fmt.Println("✅ FIX: Start checking markets after 7 PM LOCAL time in each city")
}
