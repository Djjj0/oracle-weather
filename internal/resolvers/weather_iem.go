package resolvers

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/djbro/oracle-weather/internal/config"
	"github.com/djbro/oracle-weather/pkg/polymarket"
	"github.com/djbro/oracle-weather/pkg/utils"
	pkgweather "github.com/djbro/oracle-weather/pkg/weather"
	"github.com/go-resty/resty/v2"
)

// IEMWeatherResolver uses Iowa Environmental Mesonet (matches Wunderground/Polymarket)
type IEMWeatherResolver struct {
	BaseResolver
	config     *config.Config
	client     *resty.Client
	parser     *WeatherResolver // shared parser, no DB connection — avoids SQLITE_BUSY
	cache      sync.Map
	learningDB *pkgweather.LearningDB // US cities
	intlDB     *pkgweather.LearningDB // International cities
}

// NewIEMWeatherResolver creates a new IEM weather resolver
// NewIEMWeatherResolver creates a resolver, opening the learning DBs itself.
// Prefer NewIEMWeatherResolverWithDBs when DBs are already open (avoids SQLITE_BUSY).
func NewIEMWeatherResolver(cfg *config.Config) *IEMWeatherResolver {
	learningDB, err := pkgweather.NewLearningDB("./data/learning.db")
	if err != nil {
		utils.Logger.Warnf("Failed to open learning database: %v - will use fallback timing", err)
		learningDB = nil
	}
	intlDB, err := pkgweather.NewLearningDB("./data/learning_international.db")
	if err != nil {
		utils.Logger.Warnf("IEM resolver: could not open international learning DB: %v", err)
		intlDB = nil
	}
	return NewIEMWeatherResolverWithDBs(cfg, learningDB, intlDB)
}

// NewIEMWeatherResolverWithDBs creates a resolver using pre-opened learning DBs.
// Use this to share a single DB connection across all resolver instances.
func NewIEMWeatherResolverWithDBs(cfg *config.Config, learningDB, intlDB *pkgweather.LearningDB) *IEMWeatherResolver {
	return &IEMWeatherResolver{
		config:     cfg,
		client:     resty.New().SetTimeout(15 * time.Second),
		cache:      sync.Map{},
		learningDB: learningDB,
		intlDB:     intlDB,
		parser:     &WeatherResolver{config: cfg, client: resty.New().SetTimeout(10 * time.Second)},
	}
}

// cityToAirport maps cities to their ICAO airport codes (Wunderground uses these)
var cityToAirport = map[string]string{
	"seattle":        "KSEA",
	"new york":       "KLGA", // LaGuardia — Polymarket uses KLGA not KJFK
	"new york city":  "KLGA",
	"los angeles":    "KLAX",
	"chicago":        "KORD",
	"houston":        "KIAH",
	"phoenix":        "KPHX",
	"philadelphia":   "KPHL",
	"san antonio":    "KSAT",
	"san diego":      "KSAN",
	"dallas":         "KDFW",
	"miami":          "KMIA",
	"atlanta":        "KATL",
	"boston":         "KBOS",
	"san francisco":  "KSFO",
	"denver":         "KDEN",
	"washington":     "KDCA",
	"nashville":      "KBNA",
	"london":         "EGLC", // London City Airport — Polymarket uses EGLC not EGLL
	"paris":          "LFPG", // Charles de Gaulle
	"tokyo":          "RJTT", // Haneda
	"las vegas":      "KLAS",
	"portland":       "KPDX",
	"minneapolis":    "KMSP",
	"detroit":        "KDTW",
	"austin":         "KAUS",
	"memphis":        "KMEM",
	"charlotte":      "KCLT",
	"tampa":          "KTPA",
	"orlando":        "KMCO",
	"kansas city":    "KMCI",
	"mexico city":    "MMMX", // Benito Juárez Intl
	"buenos aires":   "SAEZ", // Ezeiza
	"seoul":          "RKSI", // Incheon
	"toronto":        "CYYZ", // Pearson
	"sydney":         "YSSY",
	"melbourne":      "YMML",
	"singapore":      "WSSS",
	"hong kong":      "VHHX", // HK Observatory HQ — Polymarket resolves via HKO station, not airport (VHHH)
	"beijing":        "ZBAA",
	"shanghai":       "ZSPD",
	"dubai":          "OMDB",
	"mumbai":         "VABB",
	"delhi":          "VIDP",
	"moscow":         "UUWW", // Vnukovo — Polymarket resolves via weather.gov UUWW not UUEE (Sheremetyevo)
	"berlin":         "EDDB",
	"madrid":         "LEMD",
	"rome":           "LIRF",
	"amsterdam":      "EHAM",
	"barcelona":      "LEBL",
	"vienna":         "LOWW",
	"prague":         "LKPR",
	"stockholm":      "ESSA",
	"copenhagen":     "EKCH",
	"oslo":           "ENGM",
	"helsinki":       "EFHK",
	"brussels":       "EBBR",
	"lisbon":         "LPPT",
	"athens":         "LGAV",
	"warsaw":         "EPWA",
	"budapest":       "LHBP",
	"zurich":         "LSZH",
	"milan":          "LIMC",
	"munich":         "EDDM",
	"lucknow":        "VILK", // Chaudhary Charan Singh Intl
	"ankara":         "LTAC", // Esenboğa Intl
	"sao paulo":      "SBGR", // Guarulhos Intl
	"wellington":     "NZWN", // Wellington Intl
	// Middle East
	"tel aviv":       "LLBG", // Ben Gurion International
	"istanbul":       "LTBA", // Atatürk International (WU reference station)
	"riyadh":         "OERK", // King Khalid International
	"doha":           "OTHH", // Hamad International
	"kuwait city":    "OKBK", // Kuwait International
	// Africa
	"cairo":          "HECA", // Cairo International
	"johannesburg":   "FAOR", // O.R. Tambo International
	"cape town":      "FACT", // Cape Town International
	"lagos":          "DNMM", // Murtala Muhammed International
	"nairobi":        "HKJK", // Jomo Kenyatta International
	"casablanca":     "GMMN", // Mohammed V International
	// South / Southeast Asia
	"karachi":        "OPKC", // Jinnah International
	"lahore":         "OPLA", // Allama Iqbal International
	"chennai":        "VOMM", // Chennai International
	"kolkata":        "VECC", // Netaji Subhas Chandra Bose International
	"hyderabad":      "VOHS", // Rajiv Gandhi International
	"bangalore":      "VOBL", // Kempegowda International
	"bangkok":        "VTBS", // Suvarnabhumi
	"jakarta":        "WIII", // Soekarno-Hatta
	"manila":         "RPLL", // Ninoy Aquino International
	"ho chi minh city": "VVTS", // Tan Son Nhat
	"kuala lumpur":   "WMKK", // Kuala Lumpur International
	// East Asia
	"taipei":         "RCTP", // Taiwan Taoyuan International
	"osaka":          "RJBB", // Kansai International
	// Europe (additions)
	"frankfurt":      "EDDF", // Frankfurt Airport
	// Latin America
	"lima":           "SPJC", // Jorge Chávez International
	"bogota":         "SKBO", // El Dorado International
	"santiago":       "SCEL", // Arturo Merino Benítez International
	"caracas":        "SVMI", // Simón Bolívar International
	"guadalajara":    "MMGL", // Miguel Hidalgo International
	"monterrey":      "MMMY", // General Mariano Escobedo International
	"havana":         "MUHA", // José Martí International
	"san juan":       "TJSJ", // Luis Muñoz Marín International
}

// CheckResolution checks weather using IEM ASOS data.
//
// Strategy:
//   - IEM updates observations each hour, shortly after the hour ends.
//   - The learning DB tracks when the daily high is typically reached per city.
//   - YES bets: place as soon as the running IEM high already exceeds the threshold.
//   - NO bets: place once we are past the city's typical peak hour AND the running
//     high is clearly below the threshold (it won't recover).
//   - Rain markets: require full day (11 PM gate) since rain can occur any time.
// Name returns the human-readable name for this resolver.
func (w *IEMWeatherResolver) Name() string { return "IEM ASOS Weather" }

func (w *IEMWeatherResolver) CheckResolution(market polymarket.Market) (*string, float64, error) {
	data, err := w.ParseMarketQuestion(market.Question)
	if err != nil {
		return nil, 0, err
	}

	if data.Date.IsZero() {
		return nil, 0, nil
	}

	tzStr := getTimezone(data.Location)
	loc, err := time.LoadLocation(tzStr)
	if err != nil {
		utils.Logger.Warnf("Failed to load timezone %s for %s, using UTC: %v", tzStr, data.Location, err)
		loc = time.UTC
	}

	// Rain needs full day of data
	if data.Condition == "rain" || data.Condition == "precipitation" {
		localDate := data.Date.In(loc)
		rainGate := time.Date(localDate.Year(), localDate.Month(), localDate.Day(), 23, 0, 0, 0, loc)
		now := time.Now().In(loc)
		if now.Before(rainGate) {
			utils.Logger.Debugf("⏳ Rain market not ready: %s on %s (%.1fh until 11 PM gate)",
				data.Location, data.Date.Format("2006-01-02"), rainGate.Sub(now).Hours())
			return nil, 0, nil
		}
	}

	// Get airport code
	airportCode, ok := cityToAirport[strings.ToLower(data.Location)]
	if !ok {
		utils.Logger.Debugf("IEM: no airport mapping for city %q — skipping", data.Location)
		return nil, 0, nil
	}

	useCelsius := false
	if unit, ok := data.Extra["unit"]; ok && unit == "celsius" {
		useCelsius = true
	}
	unitSymbol := "°F"
	if useCelsius {
		unitSymbol = "°C"
	}

	// Check cache (short TTL - data updates hourly so don't cache too long)
	// Include threshold in key so adjacent exact-temp markets (27°C vs 28°C) don't collide.
	cacheKey := fmt.Sprintf("%s_%s_%s_%.1f", data.Location, data.Date.Format("2006-01-02"), data.Condition, data.Threshold)
	if cached, ok := w.cache.Load(cacheKey); ok {
		result := cached.(CachedResult)
		return &result.Outcome, result.Confidence, nil
	}

	// Rain/precipitation markets: use dedicated precipitation endpoint
	if data.Condition == "rain" || data.Condition == "precipitation" {
		totalPrecip, err := w.getIEMPrecip(airportCode, data.Date, loc)
		if err != nil {
			utils.Logger.Debugf("IEM precip transient error for %s — will retry next cycle: %v", data.Location, err)
			return nil, 0, nil
		}

		localDate := data.Date.In(loc)
		var outcome string
		if totalPrecip > 0 {
			outcome = "Yes"
		} else {
			outcome = "No"
		}
		// High confidence since we have the full day of data (gate already passed above)
		confidence := 0.92

		utils.Logger.Infof("✅ IEM rain resolved: %s on %s | Total precip: %.4f in | Outcome: %s | Confidence: %.0f%%",
			data.Location, localDate.Format("2006-01-02"), totalPrecip, outcome, confidence*100)

		w.cache.Store(cacheKey, CachedResult{Outcome: outcome, Confidence: confidence})
		return &outcome, confidence, nil
	}

	// Fetch the current running high from IEM (max of all observations so far today)
	// Pass loc so IEM uses local midnight-to-midnight, not UTC (critical fix:
	// UTC queries include the previous afternoon's readings for western timezones)
	runningHigh, obsCount, err := w.getIEMHighTemp(airportCode, data.Date, useCelsius, loc)
	if err != nil {
		utils.Logger.Debugf("IEM temp transient error for %s — will retry next cycle: %v", data.Location, err)
		return nil, 0, nil
	}

	now := time.Now().In(loc)
	currentHour := float64(now.Hour()) + float64(now.Minute())/60.0

	// If the current local calendar day has advanced past the market's UTC date
	// (e.g. Wellington UTC+13 where noon UTC = next local day), the market day is
	// completely done — force currentHour past peak so NO bets fire correctly.
	marketUTCYearDay := data.Date.UTC().YearDay()
	nowLocalYearDay := now.YearDay()
	if now.Year() > data.Date.UTC().Year() || nowLocalYearDay > marketUTCYearDay {
		currentHour = 24.0
	}

	localDate := data.Date.In(loc)

	utils.Logger.Debugf("IEM running high: %s (%s) on %s = %.1f%s at %.1f local hour (obs=%d)",
		data.Location, airportCode, data.Date.Format("2006-01-02"), runningHigh, unitSymbol, currentHour, obsCount)

	// Determine typical peak hour for this city from learning DB.
	// Returns -1 if no data — skip the market until backfill runs.
	typicalPeakHour := w.getTypicalPeakHour(data.Location)
	if typicalPeakHour < 0 {
		// No learning DB data — try live on-demand IEM lookup for last 5 days.
		targetDB := w.learningDB
		if targetDB == nil {
			targetDB = w.intlDB
		}
		typicalPeakHour = w.fetchOnDemandPeakHour(data.Location, airportCode, loc.String(), targetDB)
		if typicalPeakHour < 0 {
			utils.Logger.Debugf("IEM: on-demand peak hour lookup failed for %q — skipping", data.Location)
			return nil, 0, nil
		}
	}

	// Check for an obvious NO: running high already clearly exceeds a ceiling threshold.
	// This is safe with any number of observations — the high can only go up.
	// e.g. Lucknow running high 34°C → safe NO on 37°C, 38°C, 39°C etc. immediately.
	obviousNo, obviousConf := w.checkObviousNo(data, runningHigh)
	if obviousNo && obsCount >= 1 {
		utils.Logger.Infof("✅ IEM resolved: %s on %s | Running high: %.1f%s | Outcome: No (obvious) | Confidence: %.0f%%",
			data.Location, localDate.Format("2006-01-02"), runningHigh, unitSymbol, obviousConf*100)
		w.cache.Store(cacheKey, CachedResult{Outcome: "No", Confidence: obviousConf})
		no := "No"
		return &no, obviousConf, nil
	}

	// For all other outcomes (YES bets, or NO-after-peak bets) require enough observations
	// to trust the running high isn't stale overnight data.
	// Exception: after 8pm local the day is done — 2 obs is sufficient.
	const minObsForYes = 4
	const minObsLateDay = 2
	effectiveMinObs := minObsForYes
	if currentHour >= 20.0 {
		effectiveMinObs = minObsLateDay
	}
	if obsCount < effectiveMinObs {
		utils.Logger.Debugf("⏳ Not enough observations yet: %s on %s (%d/%d)", data.Location, data.Date.Format("2006-01-02"), obsCount, effectiveMinObs)
		return nil, 0, nil
	}

	outcome, confidence := w.determineOutcomeWithPeak(data, runningHigh, unitSymbol, currentHour, typicalPeakHour)
	if outcome == "" {
		utils.Logger.Debugf("⏳ Not yet actionable: %s | Running high: %.1f%s | Now: %.1f local | Typical peak: %.1f",
			data.Location, runningHigh, unitSymbol, currentHour, typicalPeakHour)
		return nil, 0, nil
	}

	utils.Logger.Infof("✅ IEM resolved: %s on %s | Running high: %.1f%s | Outcome: %s | Confidence: %.0f%%",
		data.Location, localDate.Format("2006-01-02"), runningHigh, unitSymbol, outcome, confidence*100)

	w.cache.Store(cacheKey, CachedResult{Outcome: outcome, Confidence: confidence})
	return &outcome, confidence, nil
}

// getTypicalPeakHour returns the optimal entry hour for a city from the learning DB.
// Returns -1 if the city has insufficient data — callers must treat -1 as "not
// actionable yet" and skip rather than guess. Run cmd/backfill-peak-hours to populate.
func (w *IEMWeatherResolver) getTypicalPeakHour(city string) float64 {
	cityLC := strings.ToLower(city)

	if w.learningDB != nil {
		if stats, err := w.learningDB.GetCityStats(cityLC); err == nil && stats.TotalMarkets >= 5 {
			return stats.OptimalEntryHour
		}
	}
	if w.intlDB != nil {
		if stats, err := w.intlDB.GetCityStats(cityLC); err == nil && stats.TotalMarkets >= 5 {
			return stats.OptimalEntryHour
		}
	}
	utils.Logger.Debugf("IEM: no peak hour data for %q — run backfill-peak-hours", cityLC)
	return -1
}


// fetchOnDemandPeakHour queries IEM for the last 5 days for a station, computes
// the average peak hour, stores the records in the learning DB, and returns the
// computed peak hour. Returns -1 if insufficient data could be fetched.
func (w *IEMWeatherResolver) fetchOnDemandPeakHour(city, station, timezone string, db *pkgweather.LearningDB) float64 {
	if db == nil {
		return -1
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		utils.Logger.Warnf("IEM on-demand: bad timezone %q for %s: %v", timezone, city, err)
		return -1
	}

	now := time.Now().UTC()
	var peakHours []float64
	for i := 1; i <= 5; i++ {
		date := now.AddDate(0, 0, -i)
		peakHour, peakTemp, err := pkgweather.FetchDailyPeak(station, date, loc)
		if err != nil {
			continue
		}
		localDate := date.In(loc)
		peakHourInt := int(peakHour)
		peakMin := int((peakHour - float64(peakHourInt)) * 60)
		highTempTime := time.Date(localDate.Year(), localDate.Month(), localDate.Day(),
			peakHourInt, peakMin, 0, 0, loc)
		marketID := fmt.Sprintf("ondemand_%s_%s", station, date.UTC().Format("2006-01-02"))
		if err := db.RecordMarketOutcome(
			marketID, strings.ToLower(city), timezone,
			date, peakTemp, highTempTime, highTempTime.Add(time.Hour), true,
		); err != nil && !strings.Contains(err.Error(), "UNIQUE") {
			utils.Logger.Warnf("IEM on-demand: DB write error for %s: %v", city, err)
		}
		peakHours = append(peakHours, peakHour)
	}

	if len(peakHours) == 0 {
		utils.Logger.Warnf("IEM on-demand: no data fetched for %s (%s)", city, station)
		return -1
	}

	var sum float64
	for _, h := range peakHours {
		sum += h
	}
	computed := sum / float64(len(peakHours))
	utils.Logger.Infof("IEM on-demand peak hour for %s: %.1f (from %d days)", city, computed, len(peakHours))
	return computed
}

// checkObviousNo returns true if the running high already makes a NO outcome
// certain regardless of time of day. Since temperature can only go up during
// the day, a running high that already exceeds a ceiling (temperature_below,
// temperature_range upper bound) is an immediate safe NO.
// Similarly, for temperature_above/range we can bet NO only when the running
// high is already above the ceiling — NOT when it's just below a threshold
// (that requires peak-hour confirmation).
func (w *IEMWeatherResolver) checkObviousNo(data *MarketData, runningHigh float64) (bool, float64) {
	roundedHigh := math.Round(runningHigh)
	switch data.Condition {
	case "temperature_below":
		// Running high already exceeds ceiling — NO is certain
		if roundedHigh > data.Threshold {
			return true, marginToConfidence(runningHigh - data.Threshold)
		}
	case "temperature_range":
		tempHigh := data.Extra["temp_high"].(float64)
		// Running high already above range ceiling — NO is certain
		if roundedHigh > tempHigh {
			return true, marginToConfidence(runningHigh - tempHigh)
		}
	case "temperature_above":
		// Cannot determine obvious NO early — temp might still rise to threshold
	case "temperature_exact":
		// Running high already exceeded the exact value — NO is certain (can't go back down)
		if roundedHigh > data.Threshold {
			return true, marginToConfidence(runningHigh - data.Threshold)
		}
	}
	return false, 0
}

// determineOutcomeWithPeak decides whether to bet YES or NO based on:
//   - The current running IEM high
//   - Whether we are past the typical peak hour (for NO bets)
func (w *IEMWeatherResolver) determineOutcomeWithPeak(data *MarketData, runningHigh float64, unitSymbol string, currentHour, typicalPeakHour float64) (string, float64) {
	roundedHigh := math.Round(runningHigh)

	// pastPeak (+1h): gate for NO bets and range/below YES bets.
	// earlyPeak (+0.5h): gate for exact-match YES bets — temp has been stable
	// long enough to be confident, but we don't need the full 1h.
	// Both give IEM time to record the actual peak observation.
	pastPeak := currentHour >= typicalPeakHour+1.0
	earlyPeak := currentHour >= typicalPeakHour+0.5

	switch data.Condition {
	case "temperature_above":
		if roundedHigh >= data.Threshold {
			// Already exceeded — safe YES bet any time
			return "Yes", marginToConfidence(runningHigh-data.Threshold)
		}
		if pastPeak {
			// Past peak and still below — won't recover, bet NO
			margin := data.Threshold - runningHigh
			// Extra confidence the further past peak we are
			peakMarginBonus := math.Min((currentHour-typicalPeakHour-1.0)*0.05, 0.10)
			return "No", math.Min(marginToConfidence(margin)+peakMarginBonus, 0.98)
		}

	case "temperature_below":
		if roundedHigh > data.Threshold {
			// Already exceeded ceiling — safe NO bet any time
			return "No", marginToConfidence(runningHigh-data.Threshold)
		}
		if pastPeak {
			margin := data.Threshold - runningHigh
			// Require at least 1 degree below ceiling before betting YES.
			// If running high == threshold, it's right at the boundary — too
			// risky (any warmer observation would exceed it). Skip.
			if margin < 1.0 {
				return "", 0
			}
			peakMarginBonus := math.Min((currentHour-typicalPeakHour-1.0)*0.05, 0.10)
			return "Yes", math.Min(marginToConfidence(margin)+peakMarginBonus, 0.98)
		}

	case "temperature_exact":
		// NO cases (safe any time):
		//   1. Running high already exceeded the exact value — it can't come back down.
		if roundedHigh > data.Threshold {
			return "No", marginToConfidence(runningHigh - data.Threshold)
		}
		if earlyPeak {
			if roundedHigh == data.Threshold {
				// Past early-peak and running high matches exactly — bet YES.
				// earlyPeak (+0.5h) is sufficient: temp has settled and won't
				// climb further in the next 30 minutes. Use pastPeak bonus.
				peakMarginBonus := math.Min((currentHour-typicalPeakHour-0.5)*0.05, 0.10)
				return "Yes", math.Min(0.80+peakMarginBonus, 0.92)
			}
			// Past peak and still below — won't reach exact value, bet NO.
			margin := data.Threshold - runningHigh
			if margin < 2.0 {
				return "", 0 // Too close to boundary — skip
			}
			peakMarginBonus := math.Min((currentHour-typicalPeakHour-1.0)*0.05, 0.10)
			return "No", math.Min(marginToConfidence(margin)+peakMarginBonus, 0.98)
		}
		return "", 0

	case "temperature_range":
		tempLow := data.Extra["temp_low"].(float64)
		tempHigh := data.Extra["temp_high"].(float64)
		if roundedHigh >= tempLow && roundedHigh <= tempHigh {
			if pastPeak {
				// In range and past peak — YES
				margin := math.Min(runningHigh-tempLow, tempHigh-runningHigh)
				return "Yes", marginToConfidence(margin)
			}
			// In range but not past peak — could still go higher and exit range
			return "", 0
		}
		if roundedHigh > tempHigh {
			// Already above range ceiling — safe NO any time
			return "No", marginToConfidence(runningHigh-tempHigh)
		}
		if pastPeak && roundedHigh < tempLow {
			// Past peak and below floor — won't reach range, bet NO
			margin := tempLow - runningHigh
			peakMarginBonus := math.Min((currentHour-typicalPeakHour-1.0)*0.05, 0.10)
			return "No", math.Min(marginToConfidence(margin)+peakMarginBonus, 0.98)
		}
	}

	return "", 0
}

// getIEMHighTemp fetches daily high temperature from IEM ASOS API.
// loc is the station's local timezone — used so the date range is midnight-to-midnight
// local time, not UTC. Without this, western-timezone stations include the previous
// afternoon's warm readings in what IEM calls "today's" UTC data.
func (w *IEMWeatherResolver) getIEMHighTemp(station string, date time.Time, celsius bool, loc *time.Location) (float64, int, error) {
	// Hong Kong: use HK Observatory Daily Extract instead of IEM ASOS.
	// Polymarket resolves HK markets using HKO "Absolute Daily Max", which is
	// published in the Daily Extract after the day ends. VHHX is not in IEM.
	if station == "VHHX" {
		temp, err := pkgweather.FetchHKODailyMax(date)
		if err != nil {
			return 0, 0, err
		}
		if temp == 0 {
			// Data not yet published — not ready
			return 0, 0, nil
		}
		// HKO returns Celsius; convert to Fahrenheit if caller expects °F
		if !celsius {
			temp = temp*9/5 + 32
		}
		return temp, 1, nil
	}

	// Use the UTC calendar date from the market question (e.g. "March 9") as the
	// year/month/day for the IEM query. The tz param tells IEM to interpret that
	// calendar date in the station's local midnight-to-midnight window.
	// Do NOT use date.In(loc) — for high-UTC-offset cities like Wellington (UTC+13)
	// noon UTC on March 9 converts to March 10 NZDT, causing IEM to query the wrong day.
	utcDate := date.UTC()
	tzName := loc.String()

	// IEM ASOS API endpoint — tz param makes IEM interpret dates in local time.
	// report_type=3 (routine METAR) and report_type=4 (SPECI) are the valid numeric
	// codes. We request both and filter SPECI client-side via the report_type column.
	url := fmt.Sprintf(
		"https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py?station=%s&data=tmpf,report_type&year1=%d&month1=%d&day1=%d&year2=%d&month2=%d&day2=%d&tz=%s&format=onlycomma&latlon=no&elev=no&missing=M&trace=T&direct=no&report_type=3&report_type=4",
		station,
		utcDate.Year(), int(utcDate.Month()), utcDate.Day(),
		utcDate.Year(), int(utcDate.Month()), utcDate.Day(),
		tzName,
	)

	resp, err := w.client.R().Get(url)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to fetch IEM data: %w", err)
	}

	if resp.IsError() {
		return 0, 0, fmt.Errorf("IEM API returned error: %s", resp.Status())
	}

	// Parse CSV response
	lines := strings.Split(strings.TrimSpace(resp.String()), "\n")
	if len(lines) < 2 {
		// No observations yet — local day may not have started. Treat as not ready.
		return 0, 0, nil
	}

	// Extract temperatures from CSV (skip header).
	// Columns: station, valid, tmpf[, report_type]
	// IEM may or may not return the report_type column; server-side filter
	// (report_type=3&report_type=4) already excludes SPECI, so we only drop
	// it client-side when the column is actually present.
	var temps []float64
	for _, line := range lines[1:] {
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}

		// Drop SPECI (report_type=4) only if the column is present.
		if len(parts) >= 4 && strings.TrimSpace(parts[3]) == "4" {
			continue
		}

		// Temperature is in 3rd column (index 2)
		tempStr := strings.TrimSpace(parts[2])
		if tempStr == "" || tempStr == "M" || tempStr == "T" || tempStr == "null" {
			continue
		}

		temp, err := strconv.ParseFloat(tempStr, 64)
		if err != nil {
			continue
		}

		// Sanity check (-100°F to 150°F)
		if temp >= -100 && temp <= 150 {
			temps = append(temps, temp)
		}
	}

	if len(temps) == 0 {
		// All rows filtered (e.g. all SPECI) or station not reporting yet.
		return 0, 0, nil
	}

	// Get daily high (max temperature)
	highTemp := temps[0]
	for _, t := range temps {
		if t > highTemp {
			highTemp = t
		}
	}

	// Convert to Celsius if needed
	if celsius {
		highTemp = (highTemp - 32) * 5 / 9
	}

	return highTemp, len(temps), nil
}

// marginToConfidence converts degrees of margin from a threshold to a confidence value.
// The further the actual temperature is from the decision boundary, the more confident we are.
func marginToConfidence(marginDegrees float64) float64 {
	switch {
	case marginDegrees >= 3.0:
		return 0.98
	case marginDegrees >= 2.0:
		return 0.95
	case marginDegrees >= 1.0:
		return 0.90
	case marginDegrees >= 0.5:
		return 0.80
	default:
		// Very close to boundary — rounding or station variance could flip the result
		return 0.70
	}
}

// getIEMPrecip fetches hourly precipitation (p01i) from IEM ASOS for a given station/date
// and returns the total precipitation in inches for that local calendar day.
// A return value > 0 means it rained; 0 means no measurable precipitation.
func (w *IEMWeatherResolver) getIEMPrecip(station string, date time.Time, loc *time.Location) (float64, error) {
	utcDate := date.UTC()
	tzName := loc.String()
	url := fmt.Sprintf(
		"https://mesonet.agron.iastate.edu/cgi-bin/request/asos.py?station=%s&data=p01i&year1=%d&month1=%d&day1=%d&year2=%d&month2=%d&day2=%d&tz=%s&format=onlycomma&latlon=no&elev=no&missing=null&trace=null&direct=no&report_type=3&report_type=4",
		station,
		utcDate.Year(), int(utcDate.Month()), utcDate.Day(),
		utcDate.Year(), int(utcDate.Month()), utcDate.Day(),
		tzName,
	)

	resp, err := w.client.R().Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch IEM precip data: %w", err)
	}
	if resp.IsError() {
		return 0, fmt.Errorf("IEM precip API returned error: %s", resp.Status())
	}

	lines := strings.Split(strings.TrimSpace(resp.String()), "\n")
	if len(lines) < 2 {
		return 0, nil
	}

	// CSV columns: station, valid, p01i
	// Sum all valid hourly precip values; skip null/missing
	var totalPrecip float64
	validReadings := 0
	for _, line := range lines[1:] {
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		valStr := strings.TrimSpace(parts[2])
		if valStr == "" || valStr == "null" || valStr == "M" {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		if val >= 0 { // negative would be bogus
			totalPrecip += val
			validReadings++
		}
	}

	if validReadings == 0 {
		return 0, nil
	}

	utils.Logger.Debugf("IEM precip: %s on %s = %.4f in total (%d readings)", station, date.Format("2006-01-02"), totalPrecip, validReadings)
	return totalPrecip, nil
}

// ParseMarketQuestion delegates to the shared parser (no DB open — avoids SQLITE_BUSY)
func (w *IEMWeatherResolver) ParseMarketQuestion(question string) (*MarketData, error) {
	return w.parser.ParseMarketQuestion(question)
}
