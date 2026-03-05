package weather

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/tebeka/selenium"
)

// WUSimpleScraper - simplified WU scraper that gets daily high/low
type WUSimpleScraper struct {
	chromeDriverPath string
}

// WUDailyTemp represents daily high/low from WU
type WUDailyTemp struct {
	Date      time.Time
	HighTempF float64
	HighTempC float64
	LowTempF  float64
	LowTempC  float64
}

// NewWUSimpleScraper creates a simple WU scraper
func NewWUSimpleScraper(chromeDriverPath string) *WUSimpleScraper {
	return &WUSimpleScraper{
		chromeDriverPath: chromeDriverPath,
	}
}

// GetDailyTemp scrapes WU for daily high/low temps
func (ws *WUSimpleScraper) GetDailyTemp(station string, date time.Time) (*WUDailyTemp, error) {
	// Start ChromeDriver (errors suppressed via Chrome args below)
	service, err := selenium.NewChromeDriverService(ws.chromeDriverPath, 9515, []selenium.ServiceOption{selenium.Output(nil)}...)
	if err != nil {
		return nil, fmt.Errorf("failed to start ChromeDriver: %w", err)
	}
	defer service.Stop()

	// Create WebDriver with stealth options + error suppression
	caps := selenium.Capabilities{
		"browserName": "chrome",
		"goog:chromeOptions": map[string]interface{}{
			"args": []string{
				"--headless=new",
				"--no-sandbox",
				"--disable-dev-shm-usage",
				"--disable-gpu",
				"--disable-blink-features=AutomationControlled",
				"--user-agent=Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
				"--ignore-certificate-errors",
				"--ignore-ssl-errors",
				"--disable-web-security",
				"--allow-running-insecure-content",
				"--log-level=3",
				"--silent",
				"--disable-logging",
				"--disable-background-networking",
				"--disable-sync",
			},
			"excludeSwitches": []string{"enable-automation", "enable-logging"},
			"useAutomationExtension": false,
		},
	}

	wd, err := selenium.NewRemote(caps, "http://localhost:9515/wd/hub")
	if err != nil {
		return nil, fmt.Errorf("failed to create WebDriver: %w", err)
	}
	defer wd.Quit()

	// Navigate to WU history page
	url := fmt.Sprintf("https://www.wunderground.com/history/daily/%s/date/%d/%d/%d",
		station, date.Year(), date.Month(), date.Day())

	if err := wd.Get(url); err != nil {
		return nil, fmt.Errorf("failed to navigate: %w", err)
	}

	// Wait for page to load (increased for Cloudflare)
	time.Sleep(12 * time.Second)

	// Get page source
	html, err := wd.PageSource()
	if err != nil {
		return nil, fmt.Errorf("failed to get page source: %w", err)
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	result := &WUDailyTemp{Date: date}

	// Find the table with High Temp and Low Temp rows
	doc.Find("tbody tr").Each(func(i int, row *goquery.Selection) {
		header := strings.TrimSpace(row.Find("th").First().Text())

		if strings.Contains(header, "High Temp") {
			// Get the first td (actual value)
			tempStr := strings.TrimSpace(row.Find("td").First().Text())
			if tempStr != "" && tempStr != "--" {
				if temp, err := strconv.ParseFloat(tempStr, 64); err == nil {
					result.HighTempF = temp
					result.HighTempC = (temp - 32) * 5 / 9
				}
			}
		}

		if strings.Contains(header, "Low Temp") {
			tempStr := strings.TrimSpace(row.Find("td").First().Text())
			if tempStr != "" && tempStr != "--" {
				if temp, err := strconv.ParseFloat(tempStr, 64); err == nil {
					result.LowTempF = temp
					result.LowTempC = (temp - 32) * 5 / 9
				}
			}
		}
	})

	if result.HighTempF == 0 {
		return nil, fmt.Errorf("no temperature data found")
	}

	return result, nil
}
