package parity_arbitrage

import (
	"sort"
)

// PriorityScorer scores opportunities for execution priority
// PHASE 8: Enhancement - Execute best opportunities first
type PriorityScorer struct {
	// Weights for scoring (must sum to 1.0)
	profitWeight    float64 // 50% weight
	liquidityWeight float64 // 30% weight
	volumeWeight    float64 // 20% weight
}

// NewPriorityScorer creates a new priority scorer
func NewPriorityScorer() *PriorityScorer {
	return &PriorityScorer{
		profitWeight:    0.50,
		liquidityWeight: 0.30,
		volumeWeight:    0.20,
	}
}

// ScoreOpportunity calculates a priority score (0-100)
func (ps *PriorityScorer) ScoreOpportunity(opp ParityOpportunity) float64 {
	// Normalize profit (assume max 20% profit = 100 points)
	profitScore := (opp.NetProfitPerDollar / 0.20) * 100
	if profitScore > 100 {
		profitScore = 100
	}

	// Normalize liquidity (assume max $10,000 = 100 points)
	liquidityScore := (opp.Liquidity / 10000) * 100
	if liquidityScore > 100 {
		liquidityScore = 100
	}

	// Normalize volume (assume max $5,000 = 100 points)
	volumeScore := (opp.Volume24h / 5000) * 100
	if volumeScore > 100 {
		volumeScore = 100
	}

	// Weighted sum
	totalScore := (profitScore * ps.profitWeight) +
		(liquidityScore * ps.liquidityWeight) +
		(volumeScore * ps.volumeWeight)

	return totalScore
}

// RankOpportunities sorts opportunities by priority score (highest first)
func (ps *PriorityScorer) RankOpportunities(opportunities []ParityOpportunity) []ScoredOpportunity {
	scored := make([]ScoredOpportunity, len(opportunities))

	for i, opp := range opportunities {
		score := ps.ScoreOpportunity(opp)
		scored[i] = ScoredOpportunity{
			Opportunity: opp,
			Score:       score,
		}
	}

	// Sort by score (highest first)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	return scored
}

// ScoredOpportunity is an opportunity with its priority score
type ScoredOpportunity struct {
	Opportunity ParityOpportunity
	Score       float64 // 0-100
}

// GetTopOpportunities returns the top N opportunities by score
func (ps *PriorityScorer) GetTopOpportunities(opportunities []ParityOpportunity, n int) []ParityOpportunity {
	if len(opportunities) == 0 {
		return []ParityOpportunity{}
	}

	ranked := ps.RankOpportunities(opportunities)

	// Return top N
	topN := n
	if len(ranked) < topN {
		topN = len(ranked)
	}

	result := make([]ParityOpportunity, topN)
	for i := 0; i < topN; i++ {
		result[i] = ranked[i].Opportunity
	}

	return result
}

// ExplainScore provides a breakdown of why an opportunity got its score
func (ps *PriorityScorer) ExplainScore(opp ParityOpportunity, score float64) string {
	profitScore := (opp.NetProfitPerDollar / 0.20) * 100
	if profitScore > 100 {
		profitScore = 100
	}
	liquidityScore := (opp.Liquidity / 10000) * 100
	if liquidityScore > 100 {
		liquidityScore = 100
	}
	volumeScore := (opp.Volume24h / 5000) * 100
	if volumeScore > 100 {
		volumeScore = 100
	}

	return "Priority Score Breakdown:\n" +
		"  Total Score: %.1f/100\n" +
		"  Profit (50%%): %.1f/100 (%.1f%% profit)\n" +
		"  Liquidity (30%%): %.1f/100 ($%.0f)\n" +
		"  Volume (20%%): %.1f/100 ($%.0f)"
}
