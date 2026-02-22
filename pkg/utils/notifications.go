package utils

import (
	"fmt"

	"github.com/go-resty/resty/v2"
)

// DiscordWebhook represents a Discord webhook message
type DiscordWebhook struct {
	Content string `json:"content"`
}

// SendDiscordNotification sends a notification to Discord
func SendDiscordNotification(webhookURL, message string) error {
	if webhookURL == "" {
		return fmt.Errorf("discord webhook URL not configured")
	}

	client := resty.New()
	webhook := DiscordWebhook{
		Content: message,
	}

	resp, err := client.R().
		SetBody(webhook).
		Post(webhookURL)

	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("discord API error: %s", resp.Status())
	}

	return nil
}

// OpportunityFoundMessage creates a formatted message for an opportunity
func OpportunityFoundMessage(marketQuestion, outcome string, currentPrice, expectedProfit float64) string {
	return fmt.Sprintf("🎯 **Opportunity Found**\n"+
		"Market: %s\n"+
		"Outcome: %s\n"+
		"Current Price: $%.2f\n"+
		"Expected Profit: %.2f%%",
		marketQuestion, outcome, currentPrice, expectedProfit*100)
}

// TradeExecutedMessage creates a formatted message for a trade
func TradeExecutedMessage(marketQuestion, outcome string, entryPrice, positionSize float64) string {
	return fmt.Sprintf("💰 **Trade Executed**\n"+
		"Market: %s\n"+
		"Outcome: %s\n"+
		"Entry Price: $%.2f\n"+
		"Position: $%.2f",
		marketQuestion, outcome, entryPrice, positionSize)
}

// DailySummaryMessage creates a daily summary message
func DailySummaryMessage(date string, totalTrades, successfulTrades int, totalProfit float64) string {
	successRate := 0.0
	if totalTrades > 0 {
		successRate = float64(successfulTrades) / float64(totalTrades) * 100
	}

	return fmt.Sprintf("📊 **Daily Summary - %s**\n"+
		"Total Trades: %d\n"+
		"Successful Trades: %d\n"+
		"Success Rate: %.1f%%\n"+
		"Total Profit: $%.2f",
		date, totalTrades, successfulTrades, successRate, totalProfit)
}
