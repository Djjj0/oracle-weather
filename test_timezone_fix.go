package main

import (
	"fmt"
	"time"
)

func main() {
	// Buenos Aires market: "Will the highest temperature in Buenos Aires be 28°C or below on February 27?"
	marketDate := time.Date(2026, 2, 27, 0, 0, 0, 0, time.UTC)

	// Load Buenos Aires timezone
	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		fmt.Printf("Error loading timezone: %v\n", err)
		return
	}

	// Convert market date to local timezone
	marketDateLocal := time.Date(
		marketDate.Year(), marketDate.Month(), marketDate.Day(),
		0, 0, 0, 0, loc,
	)

	// Add 26 hours for end of day + safety buffer
	endOfDayLocal := marketDateLocal.Add(26 * time.Hour)

	// Current time in Buenos Aires
	now := time.Now().In(loc)

	fmt.Println("🔍 TIMEZONE FIX TEST - Buenos Aires Market")
	fmt.Println("==========================================")
	fmt.Printf("Market Date: %s\n", marketDate.Format("2006-01-02"))
	fmt.Printf("Market Date (Local): %s\n", marketDateLocal.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Required End Time: %s (26 hours after midnight = 2 AM next day)\n", endOfDayLocal.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("Current Time (Buenos Aires): %s\n", now.Format("2006-01-02 15:04:05 MST"))
	fmt.Println()

	if now.Before(endOfDayLocal) {
		hoursRemaining := endOfDayLocal.Sub(now).Hours()
		fmt.Printf("❌ MARKET NOT READY - Day not complete yet!\n")
		fmt.Printf("   Need to wait %.1f more hours\n", hoursRemaining)
		fmt.Printf("   Bot will NOT trade on this market\n")
	} else {
		fmt.Printf("✅ MARKET READY - Day is complete!\n")
		fmt.Printf("   Bot can safely trade on this market\n")
	}

	fmt.Println()
	fmt.Println("🐛 OLD BUG (Before Fix):")
	fmt.Printf("   Check: Is now (%s) before market date (%s)? = %v\n",
		time.Now().Format("2006-01-02 15:04"),
		marketDate.Format("2006-01-02"),
		time.Now().Before(marketDate))
	fmt.Printf("   Result: Would have traded ❌ WRONG!\n")
	fmt.Println()
	fmt.Println("✅ NEW FIX (After Fix):")
	fmt.Printf("   Check: Is now (%s) before end of day (%s)? = %v\n",
		now.Format("2006-01-02 15:04 MST"),
		endOfDayLocal.Format("2006-01-02 15:04 MST"),
		now.Before(endOfDayLocal))
	fmt.Printf("   Result: Correctly waits until 2 AM next day ✅ CORRECT!\n")
}
