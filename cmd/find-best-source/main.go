package main

import (
	"fmt"
)

/*
COMPREHENSIVE WEATHER DATA SOURCE INVESTIGATION

Goal: Find the MOST ACCURATE data source for each international city

Cities to investigate:
1. London, UK
2. Paris, France
3. Toronto, Canada
4. Seoul, South Korea
5. Buenos Aires, Argentina
6. Ankara, Turkey
7. Sao Paulo, Brazil
8. Wellington, New Zealand

Data sources to test per city:
- Weather Underground (WU) - what Polymarket likely uses
- NOAA ISD (Integrated Surface Database) - global airport data
- Met Office (UK specific)
- Environment Canada (Canada specific)
- Local meteorological agencies
- Aviation METAR archives
- Open-Meteo (baseline for comparison)

Methodology:
1. For each city, identify ALL available data sources
2. Fetch 30 days of historical high temperature data from each source
3. Cross-validate against multiple sources to find "ground truth"
4. Calculate accuracy metrics for each source
5. Recommend the BEST source for each city

*/

type City struct {
	Name    string
	Country string
	Sources []DataSourceInfo
}

type DataSourceInfo struct {
	Name        string
	Type        string // API, Scraping, FTP, etc.
	URL         string
	StationCode string
	Cost        string // Free, Paid, etc.
	Reliability string // Official, Third-party, etc.
}

var cities = []City{
	{
		Name:    "London",
		Country: "UK",
		Sources: []DataSourceInfo{
			{
				Name:        "Met Office",
				Type:        "Official API",
				URL:         "https://www.metoffice.gov.uk/",
				StationCode: "EGLL/03772",
				Cost:        "Free with API key",
				Reliability: "Official UK Met Service",
			},
			{
				Name:        "Weather Underground",
				Type:        "Scraping/API",
				URL:         "https://www.wunderground.com/",
				StationCode: "EGLL",
				Cost:        "Free (scraping) / Paid (API)",
				Reliability: "Third-party aggregator",
			},
			{
				Name:        "NOAA ISD",
				Type:        "FTP Download",
				URL:         "https://www.ncei.noaa.gov/data/global-hourly/",
				StationCode: "037720-99999",
				Cost:        "Free",
				Reliability: "US Government archive",
			},
			{
				Name:        "Aviation Weather (METAR)",
				Type:        "API/Archive",
				URL:         "https://www.ogimet.com/",
				StationCode: "EGLL",
				Cost:        "Free",
				Reliability: "Official aviation data",
			},
			{
				Name:        "Open-Meteo",
				Type:        "API",
				URL:         "https://open-meteo.com/",
				StationCode: "51.5074,-0.1278",
				Cost:        "Free",
				Reliability: "Model-based (not observations)",
			},
		},
	},
	{
		Name:    "Paris",
		Country: "France",
		Sources: []DataSourceInfo{
			{
				Name:        "Météo-France",
				Type:        "Official API",
				URL:         "https://www.meteofrance.com/",
				StationCode: "07150",
				Cost:        "Free with registration",
				Reliability: "Official French Met Service",
			},
			{
				Name:        "Weather Underground",
				Type:        "Scraping/API",
				URL:         "https://www.wunderground.com/",
				StationCode: "LFPB",
				Cost:        "Free (scraping) / Paid (API)",
				Reliability: "Third-party aggregator",
			},
			{
				Name:        "NOAA ISD",
				Type:        "FTP Download",
				URL:         "https://www.ncei.noaa.gov/data/global-hourly/",
				StationCode: "071500-99999",
				Cost:        "Free",
				Reliability: "US Government archive",
			},
			{
				Name:        "Aviation Weather (METAR)",
				Type:        "API/Archive",
				URL:         "https://www.ogimet.com/",
				StationCode: "LFPB",
				Cost:        "Free",
				Reliability: "Official aviation data",
			},
		},
	},
	{
		Name:    "Toronto",
		Country: "Canada",
		Sources: []DataSourceInfo{
			{
				Name:        "Environment Canada",
				Type:        "Official API",
				URL:         "https://weather.gc.ca/",
				StationCode: "YYZ",
				Cost:        "Free",
				Reliability: "Official Canadian Met Service",
			},
			{
				Name:        "Weather Underground",
				Type:        "Scraping/API",
				URL:         "https://www.wunderground.com/",
				StationCode: "CYYZ",
				Cost:        "Free (scraping) / Paid (API)",
				Reliability: "Third-party aggregator",
			},
			{
				Name:        "NOAA ISD",
				Type:        "FTP Download",
				URL:         "https://www.ncei.noaa.gov/data/global-hourly/",
				StationCode: "715080-99999",
				Cost:        "Free",
				Reliability: "US Government archive",
			},
		},
	},
	{
		Name:    "Seoul",
		Country: "South Korea",
		Sources: []DataSourceInfo{
			{
				Name:        "KMA (Korea Meteorological Administration)",
				Type:        "Official API",
				URL:         "https://www.kma.go.kr/",
				StationCode: "108",
				Cost:        "Free with API key",
				Reliability: "Official Korean Met Service",
			},
			{
				Name:        "Weather Underground",
				Type:        "Scraping/API",
				URL:         "https://www.wunderground.com/",
				StationCode: "RKSS",
				Cost:        "Free (scraping) / Paid (API)",
				Reliability: "Third-party aggregator",
			},
			{
				Name:        "NOAA ISD",
				Type:        "FTP Download",
				URL:         "https://www.ncei.noaa.gov/data/global-hourly/",
				StationCode: "471080-99999",
				Cost:        "Free",
				Reliability: "US Government archive",
			},
		},
	},
	{
		Name:    "Buenos Aires",
		Country: "Argentina",
		Sources: []DataSourceInfo{
			{
				Name:        "SMN (Servicio Meteorológico Nacional)",
				Type:        "Official Website",
				URL:         "https://www.smn.gob.ar/",
				StationCode: "SAEZ",
				Cost:        "Free",
				Reliability: "Official Argentine Met Service",
			},
			{
				Name:        "Weather Underground",
				Type:        "Scraping/API",
				URL:         "https://www.wunderground.com/",
				StationCode: "SAEZ",
				Cost:        "Free (scraping) / Paid (API)",
				Reliability: "Third-party aggregator",
			},
			{
				Name:        "NOAA ISD",
				Type:        "FTP Download",
				URL:         "https://www.ncei.noaa.gov/data/global-hourly/",
				StationCode: "875760-99999",
				Cost:        "Free",
				Reliability: "US Government archive",
			},
		},
	},
	{
		Name:    "Ankara",
		Country: "Turkey",
		Sources: []DataSourceInfo{
			{
				Name:        "MGM (Turkish State Meteorological Service)",
				Type:        "Official Website",
				URL:         "https://www.mgm.gov.tr/",
				StationCode: "17130",
				Cost:        "Free",
				Reliability: "Official Turkish Met Service",
			},
			{
				Name:        "Weather Underground",
				Type:        "Scraping/API",
				URL:         "https://www.wunderground.com/",
				StationCode: "LTAC",
				Cost:        "Free (scraping) / Paid (API)",
				Reliability: "Third-party aggregator",
			},
			{
				Name:        "NOAA ISD",
				Type:        "FTP Download",
				URL:         "https://www.ncei.noaa.gov/data/global-hourly/",
				StationCode: "171300-99999",
				Cost:        "Free",
				Reliability: "US Government archive",
			},
		},
	},
	{
		Name:    "Sao Paulo",
		Country: "Brazil",
		Sources: []DataSourceInfo{
			{
				Name:        "INMET (Instituto Nacional de Meteorologia)",
				Type:        "Official API",
				URL:         "https://portal.inmet.gov.br/",
				StationCode: "A771",
				Cost:        "Free",
				Reliability: "Official Brazilian Met Service",
			},
			{
				Name:        "Weather Underground",
				Type:        "Scraping/API",
				URL:         "https://www.wunderground.com/",
				StationCode: "SBGR",
				Cost:        "Free (scraping) / Paid (API)",
				Reliability: "Third-party aggregator",
			},
			{
				Name:        "NOAA ISD",
				Type:        "FTP Download",
				URL:         "https://www.ncei.noaa.gov/data/global-hourly/",
				StationCode: "837800-99999",
				Cost:        "Free",
				Reliability: "US Government archive",
			},
		},
	},
	{
		Name:    "Wellington",
		Country: "New Zealand",
		Sources: []DataSourceInfo{
			{
				Name:        "MetService NZ",
				Type:        "Official API",
				URL:         "https://www.metservice.com/",
				StationCode: "Wellington",
				Cost:        "Paid API",
				Reliability: "Official NZ Met Service",
			},
			{
				Name:        "NIWA",
				Type:        "Official Data",
				URL:         "https://www.niwa.co.nz/",
				StationCode: "Wellington",
				Cost:        "Free (limited)",
				Reliability: "Official NZ Research",
			},
			{
				Name:        "Weather Underground",
				Type:        "Scraping/API",
				URL:         "https://www.wunderground.com/",
				StationCode: "NZWN",
				Cost:        "Free (scraping) / Paid (API)",
				Reliability: "Third-party aggregator",
			},
			{
				Name:        "NOAA ISD",
				Type:        "FTP Download",
				URL:         "https://www.ncei.noaa.gov/data/global-hourly/",
				StationCode: "934390-99999",
				Cost:        "Free",
				Reliability: "US Government archive",
			},
		},
	},
}

func main() {
	fmt.Println("=== COMPREHENSIVE WEATHER DATA SOURCE INVESTIGATION ===")
	fmt.Println("Finding the BEST data source for each international city")
	fmt.Println()

	fmt.Println("KEY INSIGHT: Polymarket likely uses Weather Underground (WU)")
	fmt.Println("Our strategy: Find sources that are MORE ACCURATE than WU")
	fmt.Println()

	fmt.Println("=== AVAILABLE DATA SOURCES PER CITY ===\n")

	for _, city := range cities {
		fmt.Printf("📍 %s, %s\n", city.Name, city.Country)
		fmt.Printf("   Available sources: %d\n", len(city.Sources))

		for i, source := range city.Sources {
			fmt.Printf("\n   %d. %s\n", i+1, source.Name)
			fmt.Printf("      Type: %s\n", source.Type)
			fmt.Printf("      Cost: %s\n", source.Cost)
			fmt.Printf("      Reliability: %s\n", source.Reliability)
			fmt.Printf("      Station: %s\n", source.StationCode)
		}
		fmt.Println()
	}

	fmt.Println("=== RECOMMENDED TESTING STRATEGY ===\n")
	fmt.Println("Phase 1: Implement fetchers for each source")
	fmt.Println("   - NOAA ISD (highest priority - authoritative, global)")
	fmt.Println("   - Weather Underground (benchmark - what Polymarket uses)")
	fmt.Println("   - Local met services (official data per country)")
	fmt.Println()
	fmt.Println("Phase 2: Fetch 30 days of data from each source")
	fmt.Println("   - Compare against multiple sources")
	fmt.Println("   - Identify consensus 'ground truth'")
	fmt.Println()
	fmt.Println("Phase 3: Calculate accuracy metrics")
	fmt.Println("   - Exact matches")
	fmt.Println("   - Average deviation")
	fmt.Println("   - Data availability")
	fmt.Println()
	fmt.Println("Phase 4: Recommend best source per city")
	fmt.Println("   - Prioritize: Official > NOAA ISD > WU")
	fmt.Println("   - Use different sources per city if needed")
	fmt.Println()

	fmt.Println("=== NEXT STEP ===")
	fmt.Println("Implement NOAA ISD fetcher - this will be the baseline")
	fmt.Println("NOAA ISD is the gold standard: authoritative, global, free")
}
