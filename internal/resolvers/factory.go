package resolvers

import (
	"strings"

	"github.com/djbro/oracle-weather/internal/config"
	pkgweather "github.com/djbro/oracle-weather/pkg/weather"
	"github.com/djbro/oracle-weather/pkg/utils"
)

// Factory creates appropriate resolvers based on market category.
// The learning DBs are opened once and shared across all IEM resolver instances
// to avoid SQLITE_BUSY errors from concurrent opens.
type Factory struct {
	config     *config.Config
	learningDB *pkgweather.LearningDB
	intlDB     *pkgweather.LearningDB
}

// NewFactory creates a new resolver factory, opening the learning DBs once.
func NewFactory(cfg *config.Config) *Factory {
	learningDB, err := pkgweather.NewLearningDB("./data/learning.db")
	if err != nil {
		utils.Logger.Warnf("Factory: could not open learning DB (peak times will use defaults): %v", err)
	}
	intlDB, err := pkgweather.NewLearningDB("./data/learning_international.db")
	if err != nil {
		utils.Logger.Warnf("Factory: could not open international learning DB: %v", err)
	}
	return &Factory{
		config:     cfg,
		learningDB: learningDB,
		intlDB:     intlDB,
	}
}

// GetResolver returns the appropriate resolver for a market category
func (f *Factory) GetResolver(marketCategory string) Resolver {
	category := strings.ToLower(marketCategory)

	switch category {
	case "weather":
		// Don't trust the category tag alone — Polymarket sometimes tags non-weather
		// markets as "weather". Let caller fall through to GetResolverByQuestion.
		return nil
	case "crypto", "cryptocurrency":
		return NewCryptoResolver(f.config)
	case "sports":
		return NewSportsResolver(f.config)
	case "soccer", "football":
		return NewSoccerResolver(f.config)
	default:
		// Try to auto-detect from category name
		return f.autoDetectResolver(category)
	}
}

// GetResolverByQuestion auto-detects category from the question text
func (f *Factory) GetResolverByQuestion(question string) Resolver {
	question = strings.ToLower(question)

	// Weather keywords
	weatherKeywords := []string{"rain", "temperature", "snow", "weather", "sunny", "cloudy"}
	for _, keyword := range weatherKeywords {
		if strings.Contains(question, keyword) {
			return NewIEMWeatherResolverWithDBs(f.config, f.learningDB, f.intlDB)
		}
	}

	// Crypto keywords (removed "price" and "doge" - too many false positives)
	// Note: "doge" matches "DOGE" (Department of Government Efficiency)
	cryptoKeywords := []string{"btc", "bitcoin", "eth", "ethereum", "crypto", "sol", "solana", "cardano", "ada", "dogecoin"}
	for _, keyword := range cryptoKeywords {
		if strings.Contains(question, keyword) {
			return NewCryptoResolver(f.config)
		}
	}

	// Special case: only match "price" if crypto coin is mentioned
	if strings.Contains(question, "price") {
		for _, coin := range []string{"btc", "bitcoin", "eth", "ethereum", "sol", "crypto"} {
			if strings.Contains(question, coin) {
				return NewCryptoResolver(f.config)
			}
		}
	}

	// Soccer keywords (HUGE category on Polymarket - Champions League, FIFA, etc.)
	soccerKeywords := []string{"advance to", "champions league", "uefa", "fifa", "real madrid", "barcelona",
		"liverpool", "manchester", "bayern", "psg", "juventus", "milan", "chelsea", "arsenal"}
	for _, keyword := range soccerKeywords {
		if strings.Contains(question, keyword) {
			return NewSoccerResolver(f.config)
		}
	}

	// US Sports keywords (NBA, NFL, etc.)
	sportsKeywords := []string{"beat", "win", "score", "game", "lakers", "celtics", "patriots", "nba", "nfl", "mlb", "nhl"}
	for _, keyword := range sportsKeywords {
		if strings.Contains(question, keyword) {
			return NewSportsResolver(f.config)
		}
	}

	// Default to nil - can't determine
	return nil
}

// autoDetectResolver tries to detect resolver from category string
func (f *Factory) autoDetectResolver(category string) Resolver {
	// Check for weather-related categories
	if strings.Contains(category, "weather") || strings.Contains(category, "climate") {
		return NewIEMWeatherResolverWithDBs(f.config, f.learningDB, f.intlDB)
	}

	// Check for crypto-related categories
	if strings.Contains(category, "crypto") || strings.Contains(category, "blockchain") {
		return NewCryptoResolver(f.config)
	}

	// Check for soccer-related categories
	if strings.Contains(category, "soccer") || strings.Contains(category, "football") ||
	   strings.Contains(category, "uefa") || strings.Contains(category, "fifa") {
		return NewSoccerResolver(f.config)
	}

	// Check for sports-related categories (US sports)
	if strings.Contains(category, "sport") || strings.Contains(category, "nba") ||
	   strings.Contains(category, "nfl") || strings.Contains(category, "mlb") {
		return NewSportsResolver(f.config)
	}

	return nil
}

// GetIEMResolver returns a shared IEM weather resolver using the factory's open DBs.
func (f *Factory) GetIEMResolver() *IEMWeatherResolver {
	return NewIEMWeatherResolverWithDBs(f.config, f.learningDB, f.intlDB)
}

// GetAllResolvers returns all available resolvers
func (f *Factory) GetAllResolvers() []Resolver {
	return []Resolver{
		NewIEMWeatherResolver(f.config),
		NewCryptoResolver(f.config),
		NewSportsResolver(f.config),
		NewSoccerResolver(f.config),
	}
}
