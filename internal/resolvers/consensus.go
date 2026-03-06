package resolvers

import (
	"fmt"
)

// ConsensusResult represents the result of checking multiple sources
type ConsensusResult struct {
	Outcome    string
	Confidence float64
	Sources    int // Number of sources that responded
	Agreement  int // Number of sources that agreed
}

// FindConsensus determines consensus from multiple results
func FindConsensus(results []string, minSources int) (*ConsensusResult, error) {
	if len(results) < minSources {
		return nil, fmt.Errorf("insufficient sources: got %d, need %d", len(results), minSources)
	}

	// Count occurrences of each outcome
	counts := make(map[string]int)
	for _, result := range results {
		counts[result]++
	}

	// Find the most common outcome
	var maxOutcome string
	maxCount := 0
	for outcome, count := range counts {
		if count > maxCount {
			maxCount = count
			maxOutcome = outcome
		}
	}

	// Calculate confidence based on agreement
	confidence := float64(maxCount) / float64(len(results))

	return &ConsensusResult{
		Outcome:    maxOutcome,
		Confidence: confidence,
		Sources:    len(results),
		Agreement:  maxCount,
	}, nil
}

// RequireConsensus checks if consensus meets minimum threshold
func RequireConsensus(results []string, minSources int, minConfidence float64) (*ConsensusResult, error) {
	consensus, err := FindConsensus(results, minSources)
	if err != nil {
		return nil, err
	}

	if consensus.Confidence < minConfidence {
		return nil, fmt.Errorf("no consensus: only %.0f%% agreement (%d/%d sources)",
			consensus.Confidence*100, consensus.Agreement, consensus.Sources)
	}

	return consensus, nil
}
