package resolvers

import (
	"strings"

	"github.com/djbro/oracle-weather/internal/config"
)

// Factory creates appropriate resolvers based on market category
type Factory struct {
	config *config.Config
}

// NewFactory creates a new resolver factory
func NewFactory(cfg *config.Config) *Factory {
	return &Factory{
		config: cfg,
	}
}

// GetResolver returns the appropriate resolver for a market category
// For Oracle Weather, we only support weather resolvers
func (f *Factory) GetResolver(marketCategory string) Resolver {
	category := strings.ToLower(marketCategory)

	if category == "weather" || category == "" {
		return NewMultiSourceWeatherResolver(f.config)
	}

	// For Oracle Weather, we only handle weather markets
	return nil
}

// GetResolverByQuestion auto-detects category from the question text
func (f *Factory) GetResolverByQuestion(question string) Resolver {
	question = strings.ToLower(question)

	// Weather keywords
	weatherKeywords := []string{"rain", "temperature", "snow", "weather", "sunny", "cloudy", "storm", "wind"}
	for _, keyword := range weatherKeywords {
		if strings.Contains(question, keyword) {
			return NewMultiSourceWeatherResolver(f.config)
		}
	}

	// Oracle Weather only handles weather markets
	return nil
}

// autoDetectResolver tries to determine resolver from category name
func (f *Factory) autoDetectResolver(category string) Resolver {
	// Check if it's weather-related
	if strings.Contains(category, "weather") ||
		strings.Contains(category, "climate") ||
		strings.Contains(category, "temperature") {
		return NewMultiSourceWeatherResolver(f.config)
	}

	// Oracle Weather only supports weather markets
	return nil
}
