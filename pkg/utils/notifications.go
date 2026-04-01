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
	expectedPnL := positionSize * expectedProfit
	pnlSign := "+"
	if expectedPnL < 0 {
		pnlSign = ""
	}
	msg := fmt.Sprintf(
		"🤖 **Trade Placed**\n"+
			"📋 **Market:** %s\n"+
			"✅ **Outcome:** %s\n"+
			"💵 **Entry price:** $%.2f\n"+
			"📐 **Cost:** $%.2f\n"+
			"📈 **Expected PNL:** %s$%.2f\n"+
			"─────────────────────\n"+
			"🧠 **Why placed:**\n",
		marketQuestion, outcome, entryPrice, positionSize, pnlSign, expectedPnL,
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

// PositionSummary is a lightweight trade record for daily report formatting.
type PositionSummary struct {
	MarketQuestion string
	Outcome        string
	EntryPrice     float64
	Cost           float64 // dollars invested
	PnL            float64 // realised profit (positive = win)
}

// DailyPnLReportMessage creates the formatted 8am daily P&L report.
// recentTrades may be nil; when non-empty, a per-trade breakdown is appended (capped at 10).
func DailyPnLReportMessage(date string, wins, losses, totalTrades int, yesterdayPnL, allTimePnL, goalAmount float64, openPositions int, recentTrades []PositionSummary) string {
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

	msg := fmt.Sprintf(
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

	if len(recentTrades) > 0 {
		msg += "\n─────────────────────────────\n📝 Yesterday's trades:\n"
		limit := len(recentTrades)
		if limit > 10 {
			limit = 10
		}
		for _, t := range recentTrades[:limit] {
			pnlSign := "+"
			if t.PnL < 0 {
				pnlSign = ""
			}
			label := t.MarketQuestion
			if len(label) > 40 {
				label = label[:37] + "..."
			}
			msg += fmt.Sprintf("  • %s → %s @ $%.2f, cost $%.2f, PNL %s$%.2f\n",
				label, t.Outcome, t.EntryPrice, t.Cost, pnlSign, t.PnL)
		}
	}

	return msg
}

// ScanSummaryMessage creates a formatted Discord message summarising a single scan cycle.
func ScanSummaryMessage(marketsScanned, withResolver, passedThreshold, opportunities int, skipReasons map[string]int) string {
	skipped := 0
	for _, v := range skipReasons {
		skipped += v
	}

	msg := fmt.Sprintf(
		"🔍 **Scan Complete**\n"+
			"─────────────────────\n"+
			"📊 Markets scanned: %d\n"+
			"🔎 Had resolver: %d\n"+
			"✅ Passed threshold: %d\n"+
			"⏭️  Skipped: %d\n",
		marketsScanned, withResolver, passedThreshold, skipped,
	)

	if len(skipReasons) > 0 {
		reasonLabels := map[string]string{
			"no_resolver":      "No resolver",
			"market_filter":    "Market filter",
			"resolution_error": "Resolution error",
			"not_resolvable":   "Not resolvable",
			"low_confidence":   "Low confidence",
			"invalid_prices":   "Invalid prices",
			"dead_market":      "Dead market",
			"low_edge":         "Edge too low",
			"no_token_id":      "No token ID",
		}
		msg += "─────────────────────\n**Skip breakdown:**\n"
		for reason, count := range skipReasons {
			label, ok := reasonLabels[reason]
			if !ok {
				label = reason
			}
			msg += fmt.Sprintf("  • %s: %d\n", label, count)
		}
	}

	msg += "─────────────────────\n"
	if opportunities > 0 {
		msg += fmt.Sprintf("🎯 **Opportunities found: %d**", opportunities)
	} else {
		msg += "💤 No opportunities this cycle"
	}
	return msg
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
