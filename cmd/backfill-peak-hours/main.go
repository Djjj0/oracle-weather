// backfill-peak-hours queries IEM ASOS for the last 90 days per city and
// populates city_stats with real peak-hour data so the bot never relies on
// hardcoded defaults.
//
// Usage:
//   go run ./cmd/backfill-peak-hours/          (uses default DB paths)
//   go run ./cmd/backfill-peak-hours/ --days 90
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	pkgweather "github.com/djbro/oracle-weather/pkg/weather"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// cityMap is the single source of truth: city → (ICAO station, IANA timezone, DB)
// db: "us" → learning.db   "intl" → learning_international.db
type cityEntry struct {
	station  string
	timezone string
	db       string
}

var cities = map[string]cityEntry{
	// US
	"seattle":        {"KSEA", "America/Los_Angeles", "us"},
	"new york":       {"KLGA", "America/New_York", "us"},
	"new york city":  {"KLGA", "America/New_York", "us"},
	"los angeles":    {"KLAX", "America/Los_Angeles", "us"},
	"chicago":        {"KORD", "America/Chicago", "us"},
	"houston":        {"KIAH", "America/Chicago", "us"},
	"phoenix":        {"KPHX", "America/Phoenix", "us"},
	"philadelphia":   {"KPHL", "America/New_York", "us"},
	"san antonio":    {"KSAT", "America/Chicago", "us"},
	"san diego":      {"KSAN", "America/Los_Angeles", "us"},
	"dallas":         {"KDFW", "America/Chicago", "us"},
	"miami":          {"KMIA", "America/New_York", "us"},
	"atlanta":        {"KATL", "America/New_York", "us"},
	"boston":         {"KBOS", "America/New_York", "us"},
	"san francisco":  {"KSFO", "America/Los_Angeles", "us"},
	"denver":         {"KDEN", "America/Denver", "us"},
	"washington":     {"KDCA", "America/New_York", "us"},
	"nashville":      {"KBNA", "America/Chicago", "us"},
	"las vegas":      {"KLAS", "America/Los_Angeles", "us"},
	"portland":       {"KPDX", "America/Los_Angeles", "us"},
	"minneapolis":    {"KMSP", "America/Chicago", "us"},
	"detroit":        {"KDTW", "America/New_York", "us"},
	"austin":         {"KAUS", "America/Chicago", "us"},
	"memphis":        {"KMEM", "America/Chicago", "us"},
	"charlotte":      {"KCLT", "America/New_York", "us"},
	"tampa":          {"KTPA", "America/New_York", "us"},
	"orlando":        {"KMCO", "America/New_York", "us"},
	"kansas city":    {"KMCI", "America/Chicago", "us"},
	// Canada
	"toronto": {"CYYZ", "America/Toronto", "intl"},
	// Latin America
	"mexico city":  {"MMMX", "America/Mexico_City", "intl"},
	"buenos aires": {"SAEZ", "America/Argentina/Buenos_Aires", "intl"},
	"sao paulo":    {"SBGR", "America/Sao_Paulo", "intl"},
	"lima":         {"SPJC", "America/Lima", "intl"},
	"bogota":       {"SKBO", "America/Bogota", "intl"},
	"santiago":     {"SCEL", "America/Santiago", "intl"},
	"caracas":      {"SVMI", "America/Caracas", "intl"},
	"guadalajara":  {"MMGL", "America/Mexico_City", "intl"},
	"monterrey":    {"MMMY", "America/Monterrey", "intl"},
	"havana":       {"MUHA", "America/Havana", "intl"},
	"san juan":     {"TJSJ", "America/Puerto_Rico", "intl"},
	// Europe
	"london":     {"EGLC", "Europe/London", "intl"},
	"paris":      {"LFPG", "Europe/Paris", "intl"},
	"berlin":     {"EDDB", "Europe/Berlin", "intl"},
	"frankfurt":  {"EDDF", "Europe/Berlin", "intl"},
	"madrid":     {"LEMD", "Europe/Madrid", "intl"},
	"barcelona":  {"LEBL", "Europe/Madrid", "intl"},
	"rome":       {"LIRF", "Europe/Rome", "intl"},
	"milan":      {"LIMC", "Europe/Rome", "intl"},
	"amsterdam":  {"EHAM", "Europe/Amsterdam", "intl"},
	"brussels":   {"EBBR", "Europe/Brussels", "intl"},
	"vienna":     {"LOWW", "Europe/Vienna", "intl"},
	"zurich":     {"LSZH", "Europe/Zurich", "intl"},
	"munich":     {"EDDM", "Europe/Berlin", "intl"},
	"prague":     {"LKPR", "Europe/Prague", "intl"},
	"warsaw":     {"EPWA", "Europe/Warsaw", "intl"},
	"budapest":   {"LHBP", "Europe/Budapest", "intl"},
	"stockholm":  {"ESSA", "Europe/Stockholm", "intl"},
	"copenhagen": {"EKCH", "Europe/Copenhagen", "intl"},
	"oslo":       {"ENGM", "Europe/Oslo", "intl"},
	"helsinki":   {"EFHK", "Europe/Helsinki", "intl"},
	"lisbon":     {"LPPT", "Europe/Lisbon", "intl"},
	"athens":     {"LGAV", "Europe/Athens", "intl"},
	"istanbul":   {"LTBA", "Europe/Istanbul", "intl"},
	"moscow":     {"UUEE", "Europe/Moscow", "intl"},
	// Middle East
	"tel aviv":    {"LLBG", "Asia/Jerusalem", "intl"},
	"ankara":      {"LTAC", "Europe/Istanbul", "intl"},
	"dubai":       {"OMDB", "Asia/Dubai", "intl"},
	"riyadh":      {"OERK", "Asia/Riyadh", "intl"},
	"doha":        {"OTHH", "Asia/Qatar", "intl"},
	"kuwait city": {"OKBK", "Asia/Kuwait", "intl"},
	// Asia
	"tokyo":             {"RJTT", "Asia/Tokyo", "intl"},
	"osaka":             {"RJBB", "Asia/Tokyo", "intl"},
	"seoul":             {"RKSI", "Asia/Seoul", "intl"},
	"beijing":           {"ZBAA", "Asia/Shanghai", "intl"},
	"shanghai":          {"ZSPD", "Asia/Shanghai", "intl"},
	"hong kong":         {"VHHH", "Asia/Hong_Kong", "intl"},
	"taipei":            {"RCTP", "Asia/Taipei", "intl"},
	"singapore":         {"WSSS", "Asia/Singapore", "intl"},
	"kuala lumpur":      {"WMKK", "Asia/Kuala_Lumpur", "intl"},
	"bangkok":           {"VTBS", "Asia/Bangkok", "intl"},
	"jakarta":           {"WIII", "Asia/Jakarta", "intl"},
	"manila":            {"RPLL", "Asia/Manila", "intl"},
	"ho chi minh city":  {"VVTS", "Asia/Ho_Chi_Minh", "intl"},
	"mumbai":            {"VABB", "Asia/Kolkata", "intl"},
	"delhi":             {"VIDP", "Asia/Kolkata", "intl"},
	"lucknow":           {"VILK", "Asia/Kolkata", "intl"},
	"chennai":           {"VOMM", "Asia/Kolkata", "intl"},
	"kolkata":           {"VECC", "Asia/Kolkata", "intl"},
	"hyderabad":         {"VOHS", "Asia/Kolkata", "intl"},
	"bangalore":         {"VOBL", "Asia/Kolkata", "intl"},
	"karachi":           {"OPKC", "Asia/Karachi", "intl"},
	"lahore":            {"OPLA", "Asia/Karachi", "intl"},
	// Africa
	"cairo":        {"HECA", "Africa/Cairo", "intl"},
	"johannesburg": {"FAOR", "Africa/Johannesburg", "intl"},
	"cape town":    {"FACT", "Africa/Johannesburg", "intl"},
	"lagos":        {"DNMM", "Africa/Lagos", "intl"},
	"nairobi":      {"HKJK", "Africa/Nairobi", "intl"},
	"casablanca":   {"GMMN", "Africa/Casablanca", "intl"},
	// Oceania
	"sydney":     {"YSSY", "Australia/Sydney", "intl"},
	"melbourne":  {"YMML", "Australia/Melbourne", "intl"},
	"wellington": {"NZWN", "Pacific/Auckland", "intl"},
}

func main() {
	days := flag.Int("days", 90, "Number of days to backfill")
	usDBPath := flag.String("us-db", "./data/learning.db", "Path to US learning DB")
	intlDBPath := flag.String("intl-db", "./data/learning_international.db", "Path to international learning DB")
	force := flag.Bool("force", false, "Re-backfill cities that already have data")
	loop := flag.Bool("loop", false, "Keep running until all cities have >= 5 days of data")
	flag.Parse()

	usDB, err := pkgweather.NewLearningDB(*usDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR opening US DB: %v\n", err)
		os.Exit(1)
	}
	defer usDB.Close()

	intlDB, err := pkgweather.NewLearningDB(*intlDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR opening intl DB: %v\n", err)
		os.Exit(1)
	}
	defer intlDB.Close()

	round := 0
	for {
		round++
		if *loop {
			fmt.Printf("\n=== Round %d ===\n", round)
		}

		now := time.Now().UTC()
		total := 0
		skipped := 0
		totalRowsAdded := 0
		remaining := 0

		for city, entry := range cities {
			db := usDB
			if entry.db == "intl" {
				db = intlDB
			}

			// Skip if already has enough real data (unless --force is set)
			if !*force {
				if stats, err := db.GetCityStats(city); err == nil && stats.TotalMarkets >= 5 {
					if round == 1 {
						fmt.Printf("  SKIP %-22s — already has %d markets in DB\n", city, stats.TotalMarkets)
					}
					skipped++
					continue
				}
			}

			fmt.Printf("  BACKFILL %-22s (%s) ...\n", city, entry.station)

			loc, err := time.LoadLocation(entry.timezone)
			if err != nil {
				fmt.Printf("    ERROR loading timezone %s: %v\n", entry.timezone, err)
				continue
			}

			added := 0
			for i := 1; i <= *days; i++ {
				date := now.AddDate(0, 0, -i)

				peakHour, peakTemp, err := fetchDailyPeak(entry.station, date, loc)
				if err != nil {
					continue
				}

				localDate := date.In(loc)
				peakHourInt := int(peakHour)
				peakMin := int((peakHour - float64(peakHourInt)) * 60)
				highTempTime := time.Date(localDate.Year(), localDate.Month(), localDate.Day(),
					peakHourInt, peakMin, 0, 0, loc)

				marketID := fmt.Sprintf("backfill_%s_%s", entry.station, date.Format("2006-01-02"))

				if err := db.RecordMarketOutcome(
					marketID, city, entry.timezone,
					date, peakTemp, highTempTime, highTempTime.Add(time.Hour), true,
				); err != nil {
					if !strings.Contains(err.Error(), "UNIQUE") {
						fmt.Printf("    WARN %s: %v\n", date.Format("2006-01-02"), err)
					}
					continue
				}
				added++
			}

			fmt.Printf("    → added %d days\n", added)
			totalRowsAdded += added
			total++

			// Check if this city still needs more data
			if stats, err := db.GetCityStats(city); err == nil && stats.TotalMarkets < 5 {
				remaining++
			}
		}

		fmt.Printf("\nRound %d done. Backfilled %d cities, skipped %d. Rows added: %d. Cities still needing data: %d\n",
			round, total, skipped, totalRowsAdded, remaining)

		if !*loop || remaining == 0 {
			break
		}

		fmt.Printf("Waiting 30s before next round...\n")
		time.Sleep(30 * time.Second)
	}
}

// fetchDailyPeak fetches hourly METAR observations from IEM for a single day
// and returns the local hour (float) and temperature when the daily max was reached.
func fetchDailyPeak(station string, date time.Time, loc *time.Location) (peakHour float64, peakTemp float64, err error) {
	utcDate := date.UTC()
	tzName := loc.String()

	url := fmt.Sprintf(
		"https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py"+
			"?station=%s&data=tmpf,report_type&year1=%d&month1=%d&day1=%d"+
			"&year2=%d&month2=%d&day2=%d&tz=%s&format=onlycomma"+
			"&latlon=no&elev=no&missing=M&trace=T&direct=no&report_type=3&report_type=4",
		station,
		utcDate.Year(), int(utcDate.Month()), utcDate.Day(),
		utcDate.Year(), int(utcDate.Month()), utcDate.Day(),
		tzName,
	)

	var body []byte
	for attempt := 1; attempt <= 3; attempt++ {
		resp, httpErr := httpClient.Get(url)
		if httpErr != nil {
			if attempt == 3 {
				return 0, 0, httpErr
			}
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			break
		}
		if attempt == 3 {
			return 0, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) < 2 {
		return 0, 0, fmt.Errorf("no data")
	}

	// CSV columns: station, valid (local time), tmpf, report_type
	// "valid" is in the format "YYYY-MM-DD HH:MM" in the requested timezone
	peakTemp = -999
	for _, line := range lines[1:] {
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		// Drop SPECI only if report_type column is present
		if len(parts) >= 4 && strings.TrimSpace(parts[3]) == "4" {
			continue
		}
		tempStr := strings.TrimSpace(parts[2])
		if tempStr == "" || tempStr == "M" || tempStr == "T" {
			continue
		}
		temp, parseErr := strconv.ParseFloat(tempStr, 64)
		if parseErr != nil || temp < -100 || temp > 150 {
			continue
		}
		if temp > peakTemp {
			peakTemp = temp
			// Parse the timestamp — IEM returns local time when tz param is set
			// Format: "2006-01-02 15:04"
			validStr := strings.TrimSpace(parts[1])
			t, parseErr := time.ParseInLocation("2006-01-02 15:04", validStr, loc)
			if parseErr == nil {
				peakHour = float64(t.Hour()) + float64(t.Minute())/60.0
			}
		}
	}

	if peakTemp == -999 {
		return 0, 0, fmt.Errorf("no valid readings")
	}

	// Convert F to C for storage consistency (learning DB stores raw high_temp)
	peakTempC := (peakTemp - 32) * 5 / 9
	return peakHour, peakTempC, nil
}
