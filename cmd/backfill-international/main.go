package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/utils"
	"github.com/djbro/oracle-weather/pkg/weather"
	"github.com/sirupsen/logrus"
)

/*
INTERNATIONAL CITIES WEATHER BACKFILL

Same methodology as US cities, but using Weather Underground (WU)
Goal: Determine optimal market entry timing for each international city

Process:
1. Fetch 1 year of historical hourly temps from Weather Underground
2. For each day, identify when high temp was reached
3. Determine when WU data "finalizes" (last update of the day)
4. Calculate optimal entry window per city
5. Store patterns in learning database

Cities:
- London, UK
- Paris, France
- Toronto, Canada
- Seoul, South Korea
- Buenos Aires, Argentina
- Ankara, Turkey
- Sao Paulo, Brazil
- Wellington, New Zealand
*/

type InternationalCity struct {
	Name      string
	Country   string
	Location  string // For Visual Crossing API (e.g., "London,UK")
	WUStation string // For reference/future WU verification
	Timezone  string
	Lat       float64
	Lon       float64
}

var cities = []InternationalCity{
	{"London", "UK", "London,UK", "EGLL", "Europe/London", 51.5074, -0.1278},
	{"Paris", "France", "Paris,France", "LFPB", "Europe/Paris", 48.8566, 2.3522},
	{"Toronto", "Canada", "Toronto,Canada", "CYYZ", "America/Toronto", 43.6532, -79.3832},
	{"Seoul", "South Korea", "Seoul,South Korea", "RKSS", "Asia/Seoul", 37.5665, 126.9780},
	{"Buenos Aires", "Argentina", "Buenos Aires,Argentina", "SAEZ", "America/Argentina/Buenos_Aires", -34.6037, -58.3816},
	{"Ankara", "Turkey", "Ankara,Turkey", "LTAC", "Europe/Istanbul", 39.9334, 32.8597},
	{"Sao Paulo", "Brazil", "Sao Paulo,Brazil", "SBGR", "America/Sao_Paulo", -23.5505, -46.6333},
	{"Wellington", "New Zealand", "Wellington,New Zealand", "NZWN", "Pacific/Auckland", -41.2865, 174.7762},
}

func main() {
	// Initialize logger
	utils.Logger = logrus.New()
	utils.Logger.SetLevel(logrus.InfoLevel)

	fmt.Println("=== INTERNATIONAL CITIES WEATHER BACKFILL ===")
	fmt.Println("Using Visual Crossing Weather API (reliable WU alternative)")
	fmt.Println("Analyzing 1 year of data to determine optimal entry timing")
	fmt.Println("ETA: 10-15 minutes (Visual Crossing is fast!)\n")
	fmt.Println("📝 NOTE: Polymarket uses Weather Underground for resolution.")
	fmt.Println("         Visual Crossing is used here for timing analysis only.")
	fmt.Println("         For live trading, we'll verify against WU.\n")

	// Load config (for validation only)
	_, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize learning database
	learningDB, err := weather.NewLearningDB("./data/learning_international.db")
	if err != nil {
		log.Fatalf("Failed to initialize learning database: %v", err)
	}
	defer learningDB.Close()

	fmt.Println("✅ Learning database initialized")

	// Get Visual Crossing API key
	vcAPIKey := os.Getenv("VISUAL_CROSSING_API_KEY")
	if vcAPIKey == "" {
		log.Fatal("Error: VISUAL_CROSSING_API_KEY environment variable not set.\n" +
			"Please sign up at https://www.visualcrossing.com/weather-api and set your API key.")
	}

	// Initialize Visual Crossing client
	vcClient := weather.NewVCClient(vcAPIKey)
	fmt.Println("✅ Visual Crossing client initialized\n")

	// Process each city
	totalProcessed := 0
	totalSuccess := 0
	totalFailed := 0

	for _, city := range cities {
		fmt.Printf("🔍 Processing: %s, %s\n", city.Name, city.Country)
		fmt.Printf("   Location: %s, Timezone: %s\n", city.Location, city.Timezone)

		successCount := 0
		failCount := 0

		// Backfill last 365 days
		for daysAgo := 1; daysAgo <= 365; daysAgo++ {
			date := time.Now().AddDate(0, 0, -daysAgo)

			pattern, err := analyzeVCMarketDay(vcClient, city, date)
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

			// Rate limiting - don't hammer WU
			time.Sleep(500 * time.Millisecond)
		}

		fmt.Printf("   ✅ Complete: %d success, %d failed\n", successCount, failCount)
		totalSuccess += successCount
		totalFailed += failCount

		// Update city statistics
		if err := learningDB.UpdateCityStats(city.Name); err != nil {
			fmt.Printf("   ⚠️  Failed to update stats: %v\n", err)
		} else {
			stats, _ := learningDB.GetCityStats(city.Name)
			if stats != nil {
				fmt.Printf("   📊 Optimal entry time: %02d:%02d %s\n",
					int(stats.OptimalEntryHour), int((stats.OptimalEntryHour-float64(int(stats.OptimalEntryHour)))*60),
					city.Timezone)
			}
		}

		fmt.Println()
	}

	// Final summary
	fmt.Println("=== BACKFILL COMPLETE ===")
	fmt.Printf("Total records processed: %d\n", totalProcessed)
	if totalProcessed > 0 {
		fmt.Printf("Success: %d (%.1f%%)\n", totalSuccess, float64(totalSuccess)/float64(totalProcessed)*100)
		fmt.Printf("Failed: %d (%.1f%%)\n\n", totalFailed, float64(totalFailed)/float64(totalProcessed)*100)
	}

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
		fmt.Printf("   Avg WU data final: %02d:%02d\n",
			int(stats.AvgIEMFinalHour), int((stats.AvgIEMFinalHour-float64(int(stats.AvgIEMFinalHour)))*60))
		fmt.Printf("   Avg market resolution: %02d:%02d\n",
			int(stats.AvgMarketResolutionHour), int((stats.AvgMarketResolutionHour-float64(int(stats.AvgMarketResolutionHour)))*60))
		fmt.Printf("   ⭐ OPTIMAL ENTRY TIME: %02d:%02d\n",
			int(stats.OptimalEntryHour), int((stats.OptimalEntryHour-float64(int(stats.OptimalEntryHour)))*60))
		fmt.Printf("   Confidence: %.1f%%\n\n", stats.ConfidenceScore*100)
	}

	fmt.Println("✅ Backfill complete! Database ready for international markets.")
	fmt.Println("📝 Next step: Update bot to use optimal entry times from database")
}

func analyzeVCMarketDay(vcClient *weather.VCClient, city InternationalCity, date time.Time) (weather.MarketPattern, error) {
	pattern := weather.MarketPattern{
		City:     city.Name,
		Date:     date,
		Timezone: city.Timezone,
	}

	// Load timezone for this city
	loc, err := time.LoadLocation(city.Timezone)
	if err != nil {
		return pattern, fmt.Errorf("failed to load timezone: %w", err)
	}
	dateInTZ := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)

	// Fetch Visual Crossing data
	dayData, err := vcClient.FetchDayData(city.Location, dateInTZ)
	if err != nil {
		return pattern, fmt.Errorf("VC fetch error: %w", err)
	}

	if len(dayData.Hours) == 0 {
		return pattern, fmt.Errorf("no hourly observations found")
	}

	// Find highest temperature and when it occurred
	var highTemp float64 = -999
	var highTempTime time.Time

	for _, hour := range dayData.Hours {
		tempC := (hour.Temp - 32) * 5 / 9 // Convert F to C
		if tempC > highTemp {
			highTemp = tempC

			// Parse hour time
			hourTime, err := time.ParseInLocation("2006-01-02 15:04:05",
				fmt.Sprintf("%s %s", dayData.Datetime, hour.Datetime), loc)
			if err == nil {
				highTempTime = hourTime
			}
		}
	}

	if highTemp == -999 {
		return pattern, fmt.Errorf("no valid temperature found")
	}

	// Last observation time = end of day (assuming data "finalizes" at end of day)
	lastHour := dayData.Hours[len(dayData.Hours)-1]
	dataFinalTime, err := time.ParseInLocation("2006-01-02 15:04:05",
		fmt.Sprintf("%s %s", dayData.Datetime, lastHour.Datetime), loc)
	if err != nil {
		// If parsing fails, assume 11:59 PM
		dataFinalTime = time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 0, 0, loc)
	}

	pattern.HighTemp = highTemp
	pattern.HighTempTime = highTempTime
	pattern.IEMDataFinalTime = dataFinalTime

	// Calculate lags
	pattern.DataLagMinutes = int(dataFinalTime.Sub(highTempTime).Minutes())

	// Market resolution time (estimated - Polymarket typically resolves shortly after data finalizes)
	marketResolutionTime := dataFinalTime.Add(30 * time.Minute)
	pattern.MarketResolvedTime = marketResolutionTime
	pattern.ResolutionLagMinutes = 30

	// Calculate optimal entry time (when data is final + safety margin)
	pattern.OptimalEntryTime = dataFinalTime.Add(15 * time.Minute)

	// Success if we have time to enter before market resolves
	pattern.Success = pattern.OptimalEntryTime.Before(marketResolutionTime)

	pattern.MarketID = fmt.Sprintf("%s_%s", city.Name, date.Format("2006-01-02"))
	pattern.Notes = "Backfilled from Visual Crossing (WU alternative for timing analysis)"

	return pattern, nil
}
