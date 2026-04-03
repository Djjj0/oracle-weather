package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	pkgweather "github.com/djbro/oracle-weather/pkg/weather"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "reset" {
		resetCities()
		return
	}
	queryCities()
}

func queryCities() {
	dbs := []struct{ name, path string }{
		{"US", "./data/learning.db"},
		{"International", "./data/learning_international.db"},
	}
	for _, d := range dbs {
		db, err := pkgweather.NewLearningDB(d.path)
		if err != nil {
			fmt.Printf("Error opening %s: %v\n", d.name, err)
			continue
		}
		defer db.Close()
		stats, err := db.GetAllCityStats()
		if err != nil {
			fmt.Printf("Error reading %s: %v\n", d.name, err)
			continue
		}
		fmt.Printf("\n=== %s DB (%d cities) ===\n", d.name, len(stats))
		fmt.Printf("%-22s  %6s  %12s  %12s  %7s  %s\n", "City", "Mkts", "AvgPeakHour", "OptimalEntry", "Success", "Timezone")
		fmt.Printf("%-22s  %6s  %12s  %12s  %7s  %s\n", "----", "----", "-----------", "------------", "-------", "--------")
		for _, s := range stats {
			fmt.Printf("%-22s  %6d  %12.2f  %12.2f  %6.1f%%  %s\n", s.City, s.TotalMarkets, s.AvgHighTempHour, s.OptimalEntryHour, s.SuccessRate*100, s.Timezone)
		}
	}
}

// citiesToReset maps city → (ICAO station, IANA timezone, db path)
var citiesToReset = []struct {
	city, station, timezone, dbPath string
}{
	{"dallas", "KDAL", "America/Chicago", "./data/learning.db"},
	{"seattle", "KSEA", "America/Los_Angeles", "./data/learning.db"},
	{"miami", "KMIA", "America/New_York", "./data/learning.db"},
}

func resetCities() {
	for _, c := range citiesToReset {
		db, err := pkgweather.NewLearningDB(c.dbPath)
		if err != nil {
			fmt.Printf("ERROR opening DB for %s: %v\n", c.city, err)
			continue
		}

		fmt.Printf("Wiping %s...\n", c.city)
		if err := db.DeleteCityData(c.city); err != nil {
			fmt.Printf("  ERROR wiping %s: %v\n", c.city, err)
			db.Close()
			continue
		}

		loc, err := time.LoadLocation(c.timezone)
		if err != nil {
			fmt.Printf("  ERROR loading timezone for %s: %v\n", c.city, err)
			db.Close()
			continue
		}

		fmt.Printf("Backfilling %s (%s) — 90 days...\n", c.city, c.station)
		added := 0
		now := time.Now().UTC()
		for i := 1; i <= 90; i++ {
			date := now.AddDate(0, 0, -i)
			peakHour, peakTemp, err := pkgweather.FetchDailyPeak(c.station, date, loc)
			if err != nil {
				continue
			}
			localDate := date.In(loc)
			peakHourInt := int(peakHour)
			peakMin := int((peakHour - float64(peakHourInt)) * 60)
			highTempTime := time.Date(localDate.Year(), localDate.Month(), localDate.Day(),
				peakHourInt, peakMin, 0, 0, loc)
			marketID := fmt.Sprintf("reset_%s_%s", c.station, date.Format("2006-01-02"))
			if err := db.RecordMarketOutcome(
				marketID, c.city, c.timezone,
				date, peakTemp, highTempTime, highTempTime.Add(time.Hour), true,
			); err != nil {
				if !strings.Contains(err.Error(), "UNIQUE") {
					fmt.Printf("  WARN %s: %v\n", date.Format("2006-01-02"), err)
				}
				continue
			}
			added++
		}
		fmt.Printf("  → added %d days\n", added)
		db.Close()
	}
	fmt.Println("\nDone. Run without args to verify.")
}
