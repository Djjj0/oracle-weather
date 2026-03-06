package utils

import (
	"fmt"
	"strings"

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

// TradeExecutedMessage creates a rich Discord notification for a placed trade.
// reasoning is a multi-line string (produced in ScanOpportunities) explaining the decision.
func TradeExecutedMessage(marketQuestion, outcome string, entryPrice, positionSize, expectedProfit, confidence float64, reasoning string) string {
	msg := fmt.Sprintf(
		"🤖 **Trade Placed**\n"+
			"📋 **Market:** %s\n"+
			"✅ **Outcome:** %s\n"+
			"💵 **Entry price:** $%.2f\n"+
			"📐 **Position size:** $%.2f\n"+
			"─────────────────────\n"+
			"🧠 **Why placed:**\n",
		marketQuestion, outcome, entryPrice, positionSize,
	)
	if reasoning != "" {
		// reasoning lines are already formatted; indent each line as a bullet
		for _, line := range strings.Split(reasoning, "\n") {
			if line == "" {
				continue
			}
			msg += fmt.Sprintf("  • %s\n", strings.TrimSpace(line))
		}
	} else {
		msg += fmt.Sprintf(
			"  • Confidence: %.0f%%\n"+
				"  • Edge leads to expected profit: %.1f%%\n",
			confidence*100, expectedProfit*100,
		)
	}
	return msg
}

// DailyPnLReportMessage creates the formatted 8am daily P&L report
func DailyPnLReportMessage(date string, wins, losses, totalTrades int, yesterdayPnL, allTimePnL, goalAmount float64, openPositions int) string {
	goalProgress := 0.0
	if goalAmount > 0 {
		goalProgress = allTimePnL / goalAmount * 100
	}

	sign := "+"
	if yesterdayPnL < 0 {
		sign = ""
	}

	allTimeSign := "+"
	if allTimePnL < 0 {
		allTimeSign = ""
	}

	return fmt.Sprintf(
		"📈 Daily P&L Report — %s\n"+
			"─────────────────────────────\n"+
			"Yesterday's trades: %d\n"+
			"✅ Wins: %d  ❌ Losses: %d\n"+
			"💰 Yesterday P&L: %s$%.2f\n"+
			"📊 All-time P&L: %s$%.2f\n"+
			"🎯 Goal progress: $%.2f / $%.2f (%.1f%%)\n"+
			"📂 Open positions: %d",
		date, totalTrades, wins, losses,
		sign, yesterdayPnL,
		allTimeSign, allTimePnL,
		allTimePnL, goalAmount, goalProgress,
		openPositions,
	)
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
