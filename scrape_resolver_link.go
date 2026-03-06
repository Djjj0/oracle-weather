package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
)

func main() {
	url := "https://polymarket.com/event/highest-temperature-in-chicago-on-february-27-2026/highest-temperature-in-chicago-on-february-27-2026-56-57f"

	fmt.Println("=== FETCHING POLYMARKET PAGE ===")
	fmt.Printf("URL: %s\n\n", url)

	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Failed to fetch page: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	html := string(body)
	fmt.Printf("Page fetched, size: %d bytes\n\n", len(html))

	// Look for wunderground.com links
	fmt.Println("=== SEARCHING FOR WUNDERGROUND LINKS ===")
	wuRegex := regexp.MustCompile(`https?://[^"'\s<>]*wunderground\.com[^"'\s<>]*`)
	matches := wuRegex.FindAllString(html, -1)

	if len(matches) > 0 {
		fmt.Printf("Found %d wunderground.com link(s):\n\n", len(matches))
		for i, match := range matches {
			fmt.Printf("%d. %s\n", i+1, match)
			stationRegex := regexp.MustCompile(`/([A-Z]{4})(?:/|$)`)
			if stationMatch := stationRegex.FindStringSubmatch(match); len(stationMatch) > 1 {
				fmt.Printf("   Station code: %s\n", stationMatch[1])
			}
		}
	} else {
		fmt.Println("No wunderground.com links found")
	}

	// Look for Market Rules section
	fmt.Println("\n=== MARKET RULES SECTION ===")
	rulesIdx := strings.Index(strings.ToLower(html), "market rules")
	if rulesIdx > 0 {
		start := rulesIdx - 200
		if start < 0 { start = 0 }
		end := rulesIdx + 1500
		if end > len(html) { end = len(html) }
		fmt.Println(html[start:end])
	}
}
