package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/tebeka/selenium"
)

// Simple WU scraper - gets high temp for a city/date
func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: get-wu-temp STATION DATE")
		fmt.Println("Example: get-wu-temp EGLL 2026-02-24")
		os.Exit(1)
	}

	station := os.Args[1]
	dateStr := os.Args[2]

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		log.Fatalf("Invalid date format: %v", err)
	}

	highTemp, err := getWUHighTemp(station, date)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Printf("%.1f\n", highTemp)
}

func getWUHighTemp(station string, date time.Time) (float64, error) {
	chromeDriverPath := "./bin/chromedriver.exe"

	// Start ChromeDriver service
	service, err := selenium.NewChromeDriverService(chromeDriverPath, 9515, []selenium.ServiceOption{selenium.Output(nil)}...)
	if err != nil {
		return 0, fmt.Errorf("failed to start ChromeDriver: %w", err)
	}
	defer service.Stop()

	// Create WebDriver
	caps := selenium.Capabilities{
		"browserName": "chrome",
		"goog:chromeOptions": map[string]interface{}{
			"args": []string{"--headless", "--no-sandbox", "--disable-dev-shm-usage"},
		},
	}

	wd, err := selenium.NewRemote(caps, "http://localhost:9515/wd/hub")
	if err != nil {
		return 0, fmt.Errorf("failed to create WebDriver: %w", err)
	}
	defer wd.Quit()

	// Navigate to WU
	url := fmt.Sprintf("https://www.wunderground.com/history/daily/%s/date/%d/%d/%d",
		station, date.Year(), date.Month(), date.Day())

	if err := wd.Get(url); err != nil {
		return 0, fmt.Errorf("failed to navigate: %w", err)
	}

	// Wait for page load
	time.Sleep(8 * time.Second)

	// Get page source
	html, err := wd.PageSource()
	if err != nil {
		return 0, fmt.Errorf("failed to get page source: %w", err)
	}

	// Parse with goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return 0, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract all wu-value spans
	var temps []float64
	doc.Find("span.wu-value.wu-value-to").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if val, err := strconv.ParseFloat(text, 64); err == nil {
			// Only keep values that look like temperatures (20-120°F)
			if val >= 20 && val <= 120 {
				temps = append(temps, val)
			}
		}
	})

	if len(temps) == 0 {
		return 0, fmt.Errorf("no temperature data found")
	}

	// The high temp is typically the highest value in the reasonable range
	highTemp := temps[0]
	for _, temp := range temps {
		if temp > highTemp {
			highTemp = temp
		}
	}

	return highTemp, nil
}
