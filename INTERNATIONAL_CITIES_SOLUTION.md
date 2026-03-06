# International Cities Weather Data Solution

## Executive Summary

After extensive research, **Weather Underground's free API was discontinued in 2018**. The public website does NOT provide historical hourly data via simple HTTP requests. However, Polymarket DOES use WU for resolution.

## The Challenge

**Polymarket Resolution Source**: Weather Underground (confirmed via [research](https://ezzekielnjuguna.medium.com/people-are-making-millions-on-polymarket-betting-on-the-weather-and-i-will-teach-you-how-24c9977b277c))
> "For London temperature markets, the resolution typically comes from Weather Underground data for a specific weather station like London City Airport, identified by the code EGLC."

**The Problem**: WU doesn't provide free historical hourly data
- Public website only shows current observations + forecasts
- Historical data returns `null` for past dates
- Enterprise API pricing not publicly available

---

## Solution Comparison

### Option 1: Visual Crossing ✅ **IMPLEMENTED**
**What**: Professional weather API with 50+ years of historical data
**Cost**: FREE tier with 1,000 records/day
**Our Usage**: 8 cities × 365 days = 2,920 records → 3 days to complete
**Accuracy**: Industry-standard WU replacement used by thousands of developers
**Speed**: Fast REST API, ~1-2 seconds per request

**Resources**:
- Website: https://www.visualcrossing.com/weather-api
- Free tier: https://www.visualcrossing.com/resources/news/announcing-our-free-weather-api-plan/
- Signup: https://www.visualcrossing.com/sign-up

**Pros**:
- ✅ Free and reliable
- ✅ Hourly historical data for 50+ years
- ✅ 1,000 records/day (enough for our needs)
- ✅ JSON API (easy to parse)
- ✅ Widely used as WU replacement
- ✅ Fast and stable

**Cons**:
- ⚠️ Might have ~0.5-1°C deviation from WU (need to validate)
- ⚠️ Not the EXACT source Polymarket uses

---

### Option 2: Weather Underground Scraping ⚠️ **FALLBACK OPTION**
**What**: Use Selenium WebDriver to bypass WU's anti-bot protection
**Cost**: FREE but high maintenance
**Speed**: SLOW (must render each page in browser)
**Complexity**: HIGH

**How It Works** ([reference](https://zperzan.github.io/projects/scrape-weather-underground/)):
1. Use Selenium to launch actual Chrome browser
2. Navigate to WU history page for each date
3. Extract temperature data from rendered HTML
4. Process one day at a time

**Implementation**:
- Go library: `github.com/tebeka/selenium`
- Requires ChromeDriver installation
- Example: https://github.com/dfreeman500/Scrape-Wunderground

**Pros**:
- ✅ Matches Polymarket exactly (same source)
- ✅ FREE (no API costs)

**Cons**:
- ❌ Very slow (2,920 page loads for full backfill)
- ❌ Fragile (WU can change HTML structure)
- ❌ Requires ChromeDriver setup
- ❌ Higher failure rate (timeouts, connection issues)
- ❌ Complex maintenance

---

### Option 3: Official National Weather Services ❌ **TOO COMPLEX**
**What**: Use each country's official weather service
**Examples**:
- UK: Met Office (DataHub API)
- France: Météo-France
- Canada: Environment Canada
- South Korea: KMA
- etc.

**Challenges**:
- Met Office only provides last 24 hours of hourly data ([source](https://www.metoffice.gov.uk/services/data/datapoint/uk-observations-detailed-documentation))
- Each country has different API, data format, access requirements
- Many require paid subscriptions for historical data
- Would need 8 different implementations

**Verdict**: Too complex for marginal accuracy gain

---

### Option 4: Other Free Weather APIs

| API | Historical Hourly | Free Tier | Notes |
|-----|------------------|-----------|-------|
| **Open-Meteo** | ✅ 80 years | Unlimited | Previously showed 1.11°C deviation from IEM |
| **WeatherAPI.com** | ✅ Limited | 1M calls/month | Good alternative to Visual Crossing |
| **Tomorrow.io** | ✅ 20 years | Limited | Worth testing |
| **OpenWeatherMap** | ❌ Paid only | N/A | Free tier excludes historical |

---

## Our Implementation Strategy

### Phase 1: Backfill with Visual Crossing ✅
**Purpose**: Learn optimal market entry timing patterns
**Source**: Visual Crossing API
**Duration**: 1 year of daily high temps for 8 cities
**Goal**: Determine when temps peak (1-4 PM) and when markets resolve (5-7 PM local)

**Why Visual Crossing**:
- Fast and reliable for large-scale backfill
- We're learning TIMING patterns, not exact temps
- Risk of small temp deviations acceptable for this phase

### Phase 2: Live Trading Verification
**For live trading, we have 2 options:**

**Option A: Consistent Visual Crossing** (RECOMMENDED FOR NOW)
- Use Visual Crossing for both backfill and live trading
- Pros: Fast, consistent, reliable
- Cons: ~0.5-1°C potential deviation from WU
- Risk: Small chance of resolving differently than Polymarket

**Option B: Hybrid Approach** (ULTIMATE ACCURACY)
- Use Visual Crossing for timing analysis (backfill)
- Use WU scraping for live trading (matches Polymarket exactly)
- Pros: Perfect alignment with Polymarket resolution
- Cons: Slower, more fragile, complex to maintain

---

## Implementation Files

### Created Files:
1. **`pkg/weather/visualcrossing.go`**
   - Visual Crossing API client
   - Functions: FetchDayData(), GetHighTemp(), GetDailyHighLow()
   - Handles timezone conversion, F to C conversion

2. **`cmd/test-visualcrossing/main.go`**
   - Test program to validate Visual Crossing API
   - Tests multiple cities
   - Shows hourly data parsing

3. **`cmd/backfill-international/main.go`** (UPDATED)
   - Changed from WU scraping to Visual Crossing API
   - Processes 8 international cities × 365 days
   - Stores patterns in `./data/learning_international.db`

### Modified Files:
- Added Location field to InternationalCity struct
- Updated analyzeMarketDay to use Visual Crossing

---

## How to Run

### Step 1: Get Visual Crossing API Key
```bash
# Sign up at:
https://www.visualcrossing.com/weather-api

# Free tier includes:
# - 1,000 records/day
# - Hourly historical data
# - No credit card required
```

### Step 2: Set Environment Variable
```bash
export VISUAL_CROSSING_API_KEY='your_api_key_here'
```

### Step 3: Test the API
```bash
cd /c/Users/djbro/.local/bin/oracle-weather
go run cmd/test-visualcrossing/main.go
```

**Expected Output**:
```
✅ Successfully fetched data
   Date: 2026-02-24
   Daily High: 62.0°F (16.7°C)
   Daily Low: 48.0°F (8.9°C)
   Hourly observations: 24

📈 High Temperature: 16.7°C at 14:30 GMT
✅ Paris: 12.3°C at 15:45 CET
✅ Toronto: 4.2°C at 14:15 EST
✅ Seoul: 8.9°C at 15:00 KST
```

### Step 4: Run 1-Year Backfill
```bash
# This will take 3 days to complete (1,000 records/day free tier)
# Or run in batches

go run cmd/backfill-international/main.go
```

**Expected Output**:
```
🔍 Processing: London, UK
   Location: London,UK, Timezone: Europe/London
   📈 Progress: 30/365 days processed
   📈 Progress: 60/365 days processed
   ...
   ✅ Complete: 365 success, 0 failed
   📊 Optimal entry time: 16:30 Europe/London

🔍 Processing: Paris, France
   ...
```

---

## Validation Plan

### After Backfill Completes:
1. **Compare VC vs WU for recent dates** (manual spot check)
   - Pick 5 random cities and dates
   - Compare VC high temp vs WU historical page
   - Calculate deviation

2. **If deviation < 1°C**: Use VC for live trading (acceptable risk)
3. **If deviation > 1°C**: Implement WU scraping with Selenium for live trades

---

## Future Enhancements

### If We Need Perfect WU Alignment:
1. Implement WU Selenium scraper using `tebeka/selenium`
2. Use ONLY for live trading (not backfill)
3. Scrape WU once per day per city to verify VC accuracy
4. If VC consistently matches WU → stick with VC
5. If VC shows deviations → switch to WU scraping

### Resources for WU Scraping:
- Go Selenium: https://github.com/tebeka/selenium
- Python example: https://github.com/dfreeman500/Scrape-Wunderground
- Tutorial: https://zperzan.github.io/projects/scrape-weather-underground/

---

## Key Decisions

✅ **Use Visual Crossing for 1-year backfill** (fast, reliable)
✅ **Use Visual Crossing for initial live trading** (consistent)
🔄 **Monitor VC vs WU accuracy** (validate assumption)
⏭️ **Implement WU scraping IF needed** (fallback plan)

---

## Cities Configuration

| City | Country | VC Location | WU Station | Timezone |
|------|---------|-------------|------------|----------|
| London | UK | London,UK | EGLL | Europe/London |
| Paris | France | Paris,France | LFPB | Europe/Paris |
| Toronto | Canada | Toronto,Canada | CYYZ | America/Toronto |
| Seoul | South Korea | Seoul,South Korea | RKSS | Asia/Seoul |
| Buenos Aires | Argentina | Buenos Aires,Argentina | SAEZ | America/Argentina/Buenos_Aires |
| Ankara | Turkey | Ankara,Turkey | LTAC | Europe/Istanbul |
| Sao Paulo | Brazil | Sao Paulo,Brazil | SBGR | America/Sao_Paulo |
| Wellington | New Zealand | Wellington,New Zealand | NZWN | Pacific/Auckland |

---

## References

- [Visual Crossing API Documentation](https://www.visualcrossing.com/resources/documentation/weather-api/timeline-weather-api/)
- [WU Alternative Options](https://www.getambee.com/blogs/weather-underground-alternative)
- [Polymarket Weather Trading Guide](https://ezzekielnjuguna.medium.com/people-are-making-millions-on-polymarket-betting-on-the-weather-and-i-will-teach-you-how-24c9977b277c)
- [WU Scraping Tutorial](https://zperzan.github.io/projects/scrape-weather-underground/)
- [Selenium in Go](https://github.com/tebeka/selenium)

---

## Next Steps

1. ✅ Get Visual Crossing API key
2. ✅ Test API with `cmd/test-visualcrossing`
3. ⏭️ Run 1-year backfill (`cmd/backfill-international`)
4. ⏭️ Analyze timing patterns (optimal entry windows)
5. ⏭️ Validate VC accuracy vs WU (spot checks)
6. ⏭️ Deploy bot with learned timing patterns
7. ⏭️ Monitor performance and VC/WU alignment
8. ⏭️ Implement WU scraping IF deviations detected
