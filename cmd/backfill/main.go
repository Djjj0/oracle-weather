package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/djbro/oracle-weather/pkg/weather"
	"github.com/go-resty/resty/v2"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize logger
	utils.Logger = logrus.New()
	utils.Logger.SetLevel(logrus.InfoLevel)

	fmt.Println("=== WEATHER MARKET HISTORICAL BACKFILL ===")
	fmt.Println("This will analyze 1 year of weather market data")
	fmt.Println("ETA: 30-60 minutes depending on API speed\n")

	// Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize learning database
	learningDB, err := weather.NewLearningDB("./data/learning.db")
	if err != nil {
		log.Fatalf("Failed to initialize learning database: %v", err)
	}
	defer learningDB.Close()

	fmt.Println("✅ Learning database initialized")

	// Initialize Polymarket client
	client := polymarket.NewClient(cfg)
	fmt.Println("✅ Polymarket client initialized")

	// Initialize IEM client
	iemClient := resty.New().SetTimeout(30 * time.Second)
	fmt.Println("✅ IEM client initialized\n")

	// Fetch all active markets to understand structure
	fmt.Println("📊 Fetching current weather markets to identify cities...")
	markets, err := client.GetActiveMarkets()
	if err != nil {
		log.Fatalf("Failed to fetch markets: %v", err)
	}

	// Identify all weather markets and extract cities
	cities := make(map[string]bool)
	weatherMarketCount := 0

	for _, market := range markets {
		if isWeatherMarket(market.Question) {
			city := extractCityFromQuestion(market.Question)
			if city != "" {
				cities[city] = true
				weatherMarketCount++
			}
		}
	}

	fmt.Printf("✅ Found %d weather markets across %d cities\n\n", weatherMarketCount, len(cities))

	// Process each city
	totalProcessed := 0
	totalSuccess := 0
	totalFailed := 0

	for city := range cities {
		fmt.Printf("🔍 Processing: %s\n", strings.Title(city))

		// Get timezone for city
		station, timezone, err := learningDB.GetCityTimezone(city)
		if err != nil {
			fmt.Printf("   ⚠️  City not in database, skipping: %v\n\n", err)
			continue
		}

		fmt.Printf("   Station: %s, Timezone: %s\n", station, timezone)

		// Backfill last 365 days
		successCount := 0
		failCount := 0

		for daysAgo := 1; daysAgo <= 365; daysAgo++ {
			date := time.Now().AddDate(0, 0, -daysAgo)

			pattern, err := analyzeMarketDay(iemClient, city, station, timezone, date)
			if err != nil {
				failCount++
				if daysAgo <= 7 {
					fmt.Printf("   ❌ Day -%d: %v\n", daysAgo, err)
				}
				continue
			}

			// Store in database
			if err := learningDB.AddMarketPattern(pattern); err != nil {
				fmt.Printf("   ❌ Failed to store pattern: %v\n", err)
				failCount++
				continue
			}

			successCount++
			totalProcessed++

			// Progress update every 30 days
			if daysAgo%30 == 0 {
				fmt.Printf("   📈 Progress: %d/%d days processed\n", daysAgo, 365)
			}
		}

		fmt.Printf("   ✅ Complete: %d success, %d failed\n", successCount, failCount)
		totalSuccess += successCount
		totalFailed += failCount

		// Update city statistics
		if err := learningDB.UpdateCityStats(city); err != nil {
			fmt.Printf("   ⚠️  Failed to update stats: %v\n", err)
		} else {
			stats, _ := learningDB.GetCityStats(city)
			if stats != nil {
				fmt.Printf("   📊 Optimal entry time: %02d:%02d %s\n",
					int(stats.OptimalEntryHour), int((stats.OptimalEntryHour-float64(int(stats.OptimalEntryHour)))*60),
					timezone)
			}
		}

		fmt.Println()
	}

	// Final summary
	fmt.Println("=== BACKFILL COMPLETE ===")
	fmt.Printf("Total records processed: %d\n", totalProcessed)
	fmt.Printf("Success: %d (%.1f%%)\n", totalSuccess, float64(totalSuccess)/float64(totalProcessed)*100)
	fmt.Printf("Failed: %d (%.1f%%)\n\n", totalFailed, float64(totalFailed)/float64(totalProcessed)*100)

	// Display city statistics
	fmt.Println("=== CITY STATISTICS ===\n")
	allStats, err := learningDB.GetAllCityStats()
	if err != nil {
		log.Fatalf("Failed to get stats: %v", err)
	}

	for _, stats := range allStats {
		fmt.Printf("📍 %s (%s)\n", strings.Title(stats.City), stats.Timezone)
		fmt.Printf("   Data points: %d\n", stats.TotalMarkets)
		fmt.Printf("   Avg high temp time: %02d:%02d\n",
			int(stats.AvgHighTempHour), int((stats.AvgHighTempHour-float64(int(stats.AvgHighTempHour)))*60))
		fmt.Printf("   Avg IEM data final: %02d:%02d\n",
			int(stats.AvgIEMFinalHour), int((stats.AvgIEMFinalHour-float64(int(stats.AvgIEMFinalHour)))*60))
		fmt.Printf("   Avg market resolution: %02d:%02d\n",
			int(stats.AvgMarketResolutionHour), int((stats.AvgMarketResolutionHour-float64(int(stats.AvgMarketResolutionHour)))*60))
		fmt.Printf("   ⭐ OPTIMAL ENTRY TIME: %02d:%02d\n",
			int(stats.OptimalEntryHour), int((stats.OptimalEntryHour-float64(int(stats.OptimalEntryHour)))*60))
		fmt.Printf("   Confidence: %.1f%%\n\n", stats.ConfidenceScore*100)
	}

	fmt.Println("✅ Backfill complete! Database ready for bot to use.")
	fmt.Println("📝 Next step: Update bot to use optimal entry times from database")
}

func analyzeMarketDay(client *resty.Client, city, station, timezone string, date time.Time) (weather.MarketPattern, error) {
	pattern := weather.MarketPattern{
		City:     city,
		Date:     date,
		Timezone: timezone,
	}

	// Fetch IEM data for this day
	dateStr := date.Format("2006-01-02")

	url := fmt.Sprintf("https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py?station=%s&data=tmpf&year1=%s&month1=%s&day1=%s&year2=%s&month2=%s&day2=%s&tz=Etc/UTC&format=onlycomma&latlon=no&elev=no&missing=null&trace=null&direct=no&report_type=1&report_type=2",
		station,
		dateStr[:4], dateStr[5:7], dateStr[8:10],
		dateStr[:4], dateStr[5:7], dateStr[8:10])

	resp, err := client.R().Get(url)
	if err != nil {
		return pattern, fmt.Errorf("IEM API error: %w", err)
	}

	// Parse CSV response
	observations := parseIEMData(resp.String())
	if len(observations) == 0 {
		return pattern, fmt.Errorf("no observations found")
	}

	// Find highest temperature
	var highTemp float64 = -999
	var highTempTime time.Time

	for _, obs := range observations {
		if obs.Temp > highTemp {
			highTemp = obs.Temp
			highTempTime = obs.Time
		}
	}

	// Last observation time = when data became "final"
	iemDataFinalTime := observations[len(observations)-1].Time

	pattern.HighTemp = highTemp
	pattern.HighTempTime = highTempTime
	pattern.IEMDataFinalTime = iemDataFinalTime

	// Calculate lags
	pattern.DataLagMinutes = int(iemDataFinalTime.Sub(highTempTime).Minutes())

	// For now, assume market resolved around midnight (we don't have actual resolution time from API)
	// In live mode, bot will log actual resolution times
	marketResolutionTime := time.Date(date.Year(), date.Month(), date.Day(), 23, 45, 0, 0, date.Location())
	pattern.MarketResolvedTime = marketResolutionTime
	pattern.ResolutionLagMinutes = int(marketResolutionTime.Sub(iemDataFinalTime).Minutes())

	// Calculate optimal entry time (30 min after IEM data final)
	pattern.OptimalEntryTime = iemDataFinalTime.Add(30 * time.Minute)

	// Would we have caught this opportunity?
	pattern.Success = pattern.OptimalEntryTime.Before(marketResolutionTime)

	pattern.MarketID = fmt.Sprintf("%s_%s", city, dateStr)
	pattern.Notes = "Backfilled from historical data"

	return pattern, nil
}

type IEMObservation struct {
	Time time.Time
	Temp float64
}

func parseIEMData(csv string) []IEMObservation {
	lines := strings.Split(csv, "\n")
	if len(lines) < 2 {
		return nil
	}

	var observations []IEMObservation
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}

		// CSV format: station,valid,tmpf
		timestamp := parts[1]
		tempStr := parts[2]

		if tempStr == "null" || tempStr == "" {
			continue
		}

		t, err := time.Parse("2006-01-02 15:04", timestamp)
		if err != nil {
			continue
		}

		var temp float64
		fmt.Sscanf(tempStr, "%f", &temp)

		observations = append(observations, IEMObservation{
			Time: t,
			Temp: temp,
		})
	}

	return observations
}

func isWeatherMarket(question string) bool {
	q := strings.ToLower(question)
	return strings.Contains(q, "temperature") ||
		strings.Contains(q, "highest") ||
		strings.Contains(q, "high temp")
}

func extractCityFromQuestion(question string) string {
	q := strings.ToLower(question)

	cities := []string{"seattle", "chicago", "miami", "new york", "phoenix",
		"los angeles", "denver", "dallas", "houston", "atlanta",
		"boston", "san francisco"}

	for _, city := range cities {
		if strings.Contains(q, city) {
			return city
		}
	}

	return ""
}
