package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/tebeka/selenium"
)

func main() {
	fmt.Println("=== Capturing WU HTML for Analysis ===")
	fmt.Println()

	chromeDriverPath := "./bin/chromedriver.exe"

	// Start ChromeDriver
	opts := []selenium.ServiceOption{selenium.Output(nil)}
	service, err := selenium.NewChromeDriverService(chromeDriverPath, 9515, opts...)
	if err != nil {
		log.Fatalf("Failed to start ChromeDriver: %v", err)
	}
	defer service.Stop()

	// Create WebDriver
	caps := selenium.Capabilities{
		"browserName": "chrome",
		"goog:chromeOptions": map[string]interface{}{
			"args": []string{
				"--headless",
				"--no-sandbox",
				"--disable-dev-shm-usage",
			},
		},
	}

	wd, err := selenium.NewRemote(caps, "http://localhost:9515/wd/hub")
	if err != nil {
		log.Fatalf("Failed to create WebDriver: %v", err)
	}
	defer wd.Quit()

	// Navigate to WU
	yesterday := time.Now().AddDate(0, 0, -2)
	url := fmt.Sprintf(
		"https://www.wunderground.com/history/daily/EGLL/date/%d/%d/%d",
		yesterday.Year(), yesterday.Month(), yesterday.Day(),
	)

	fmt.Printf("Fetching: %s\n", url)
	if err := wd.Get(url); err != nil {
		log.Fatalf("Failed to navigate: %v", err)
	}

	// Wait for page load
	time.Sleep(8 * time.Second)

	// Get page source
	html, err := wd.PageSource()
	if err != nil {
		log.Fatalf("Failed to get page source: %v", err)
	}

	// Save to file
	filename := "wu_page_source.html"
	if err := os.WriteFile(filename, []byte(html), 0644); err != nil {
		log.Fatalf("Failed to write file: %v", err)
	}

	fmt.Printf("\n✅ HTML saved to: %s\n", filename)
	fmt.Printf("   Size: %d bytes\n", len(html))
	fmt.Println()
	fmt.Println("Now searching for temperature patterns...")
	fmt.Println()

	// Search for temperature-related patterns
	patterns := []string{
		"Temperature",
		"wu-value",
		"temp",
		"high",
		"low",
		"history",
		"observation",
	}

	for _, pattern := range patterns {
		count := 0
		for i := 0; i < len(html)-len(pattern); i++ {
			if html[i:i+len(pattern)] == pattern {
				count++
			}
		}
		if count > 0 {
			fmt.Printf("Found '%s': %d times\n", pattern, count)
		}
	}
}
