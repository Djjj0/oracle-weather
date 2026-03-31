package polymarket

import (
	"encoding/json"
	"strconv"
)

// parseJSONStringArray parses a JSON array string into a string slice.
func parseJSONStringArray(str string) []string {
	var result []string
	if err := json.Unmarshal([]byte(str), &result); err != nil {
		return []string{}
	}
	return result
}

// parseOutcomePrices parses the outcome prices JSON string from the Gamma API
// into a float64 slice.
func parseOutcomePrices(pricesStr string) []float64 {
	var prices []string
	if err := json.Unmarshal([]byte(pricesStr), &prices); err != nil {
		return []float64{}
	}
	result := make([]float64, 0, len(prices))
	for _, p := range prices {
		if f, err := strconv.ParseFloat(p, 64); err == nil {
			result = append(result, f)
		}
	}
	return result
}
