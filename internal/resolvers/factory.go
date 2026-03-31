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

// GetResolver returns the appropriate resolver for a market category.
// Only weather markets are supported — all other categories return nil.
func (f *Factory) GetResolver(marketCategory string) Resolver {
	category := strings.ToLower(marketCategory)
	if strings.Contains(category, "weather") || strings.Contains(category, "climate") {
		return NewIEMWeatherResolverWithDBs(f.config, f.learningDB, f.intlDB)
	}
	return nil
}

// containsWord checks if a question contains a keyword as a whole word (not a substring).
// e.g. "rain" matches "will it rain" but NOT "rainbow".
func containsWord(question, keyword string) bool {
	idx := strings.Index(question, keyword)
	if idx < 0 {
		return false
	}
	// Check char before keyword
	if idx > 0 {
		c := question[idx-1]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			return false
		}
	}
	// Check char after keyword
	end := idx + len(keyword)
	if end < len(question) {
		c := question[end]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// GetResolverByQuestion auto-detects the resolver from the question text.
// Only weather questions are supported.
func (f *Factory) GetResolverByQuestion(question string) Resolver {
	question = strings.ToLower(question)

	weatherKeywords := []string{"rain", "temperature", "snow", "weather", "sunny", "cloudy"}
	for _, keyword := range weatherKeywords {
		if containsWord(question, keyword) {
			return NewIEMWeatherResolverWithDBs(f.config, f.learningDB, f.intlDB)
		}
	}

	return nil
}

// GetIEMResolver returns a shared IEM weather resolver using the factory's open DBs.
func (f *Factory) GetIEMResolver() *IEMWeatherResolver {
	return NewIEMWeatherResolverWithDBs(f.config, f.learningDB, f.intlDB)
}

// LearningDBForCity returns the learning DB that covers the given city
// (US DB first, then international). Returns nil if neither is open.
func (f *Factory) LearningDBForCity(city string) *pkgweather.LearningDB {
	cityLC := strings.ToLower(city)
	if f.learningDB != nil {
		if _, err := f.learningDB.GetCityStats(cityLC); err == nil {
			return f.learningDB
		}
	}
	if f.intlDB != nil {
		if _, err := f.intlDB.GetCityStats(cityLC); err == nil {
			return f.intlDB
		}
	}
	// Default to US DB (will create a new city record on first write)
	if f.learningDB != nil {
		return f.learningDB
	}
	return f.intlDB
}

// GetAllResolvers returns all available resolvers.
func (f *Factory) GetAllResolvers() []Resolver {
	return []Resolver{
		NewIEMWeatherResolverWithDBs(f.config, f.learningDB, f.intlDB),
	}
}
