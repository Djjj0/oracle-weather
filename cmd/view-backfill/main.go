// view-backfill prints a formatted summary of the learning databases so you can
// confirm what was stored after running cmd/backfill-peak-hours.
//
// Usage:
//
//	go run ./cmd/view-backfill/
//	go run ./cmd/view-backfill/ --city london
//	go run ./cmd/view-backfill/ --us-db ./data/learning.db --intl-db ./data/learning_international.db
package main

import (
	"flag"
	"fmt"
	"os"

	pkgweather "github.com/djbro/oracle-weather/pkg/weather"
)

func main() {
	usDBPath   := flag.String("us-db",   "./data/learning.db",               "Path to US learning DB")
	intlDBPath := flag.String("intl-db", "./data/learning_international.db", "Path to international learning DB")
	cityFlag   := flag.String("city",    "",                                  "Drill into a single city")
	flag.Parse()

	usDB, err := pkgweather.NewLearningDB(*usDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR opening US DB (%s): %v\n", *usDBPath, err)
		os.Exit(1)
	}
	defer usDB.Close()

	intlDB, err := pkgweather.NewLearningDB(*intlDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR opening intl DB (%s): %v\n", *intlDBPath, err)
		os.Exit(1)
	}
	defer intlDB.Close()

	if *cityFlag != "" {
		drillCity(*cityFlag, usDB, intlDB)
		return
	}

	printSummary("US", usDB)
	fmt.Println()
	printSummary("INTL", intlDB)
}

func printSummary(label string, db *pkgweather.LearningDB) {
	summaries, err := db.GetHistoryCitySummaries()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR querying %s DB: %v\n", label, err)
		return
	}

	if len(summaries) == 0 {
		fmt.Printf("%s DB: no data found\n", label)
		return
	}

	fmt.Printf("=== %s DB (%d cities) ===\n", label, len(summaries))
	fmt.Printf("%-22s | %-5s | %-23s | %-14s | %s\n",
		"CITY", "ROWS", "DATE RANGE", "AVG PEAK HOUR", "CONFIDENCE")
	fmt.Println("───────────────────────|───────|─────────────────────────|────────────────|───────────")
	for _, s := range summaries {
		dateRange := s.MinDate + " – " + s.MaxDate
		peakH := int(s.AvgPeakHour)
		peakM := int((s.AvgPeakHour - float64(peakH)) * 60)
		fmt.Printf("%-22s | %-5d | %-23s | %02d:%02d local      | %.2f\n",
			s.City, s.Count, dateRange, peakH, peakM, s.Confidence)
	}
}

func drillCity(city string, usDB, intlDB *pkgweather.LearningDB) {
	patterns, err := usDB.GetCityMarketHistory(city)
	dbLabel := "US"
	if err != nil || len(patterns) == 0 {
		patterns, err = intlDB.GetCityMarketHistory(city)
		dbLabel = "INTL"
	}
	if err != nil || len(patterns) == 0 {
		fmt.Printf("No data found for %q in either DB\n", city)
		return
	}

	fmt.Printf("=== %q in %s DB — %d rows ===\n", city, dbLabel, len(patterns))
	fmt.Printf("%-12s | %-9s | %-7s | %-12s | %s\n",
		"DATE", "HIGH°C", "PEAK_HR", "DATA_LAG_MIN", "SUCCESS")
	fmt.Println("─────────────|──────────|─────────|──────────────|────────")
	for _, p := range patterns {
		peakHour := float64(p.HighTempTime.Hour()) + float64(p.HighTempTime.Minute())/60.0
		fmt.Printf("%-12s | %-9.1f | %-7.2f | %-12d | %v\n",
			p.Date.Format("2006-01-02"), p.HighTemp, peakHour, p.DataLagMinutes, p.Success)
	}
}
