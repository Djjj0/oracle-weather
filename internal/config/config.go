package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the bot
type Config struct {
	// Polymarket
	PolymarketAPIKey        string
	PolymarketAPISecret     string
	PolymarketAPIPassphrase string
	PolymarketPrivateKey    string
	PolymarketBaseURL       string // CLOB API for trading
	PolymarketGammaURL      string // Gamma API for market data

	// Blockchain
	AlchemyAPIKey  string
	PolygonRPCURL  string
	ChainID        int

	// Weather APIs (multi-source validation)
	OpenWeatherAPIKey      string
	OpenWeatherBaseURL     string
	VisualCrossingAPIKey   string
	VisualCrossingBaseURL  string
	OpenMeteoBaseURL       string

	// Sports
	OddsAPIKey    string
	ESPNBaseURL   string

	// Crypto
	CoinGeckoBaseURL string

	// Bot Settings
	MinProfitThreshold float64
	MaxPositionSize    float64
	CheckInterval      int
	LogLevel           string
	DatabasePath       string

	// Circuit Breaker Settings
	DailyLossLimit  float64
	DailyTradeLimit int

	// Market Quality Filters
	MinLiquidity  float64
	MinVolume     float64
	MinMarketAge  int     // hours
	MaxSpread     float64 // percentage

	// Parity Arbitrage Settings
	ParityEnabled               bool
	ParityMinProfitThreshold    float64
	ParityMinSpread             float64
	ParityMaxPositionSize       float64
	ParityScanIntervalSeconds   int
	ParityMinLiquidity          float64
	ParityMinVolume24h          float64
	ParityMaxConcurrentPositions int

	// Notifications
	DiscordWebhookURL string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() (*Config, error) {
	// Load .env file if it exists
	_ = godotenv.Load()

	cfg := &Config{
		// Polymarket
		PolymarketAPIKey:        os.Getenv("POLYMARKET_API_KEY"),
		PolymarketAPISecret:     os.Getenv("POLYMARKET_API_SECRET"),
		PolymarketAPIPassphrase: os.Getenv("POLYMARKET_API_PASSPHRASE"),
		PolymarketPrivateKey:    os.Getenv("POLYMARKET_PRIVATE_KEY"),
		PolymarketBaseURL:       getEnvOrDefault("POLYMARKET_BASE_URL", "https://clob.polymarket.com"),
		PolymarketGammaURL:      getEnvOrDefault("POLYMARKET_GAMMA_URL", "https://gamma-api.polymarket.com"),

		// Blockchain
		AlchemyAPIKey: os.Getenv("ALCHEMY_API_KEY"),
		PolygonRPCURL: os.Getenv("POLYGON_RPC_URL"),
		ChainID:       getEnvAsInt("CHAIN_ID", 137),

		// Weather APIs (multi-source validation)
		OpenWeatherAPIKey:     os.Getenv("OPENWEATHER_API_KEY"),
		OpenWeatherBaseURL:    getEnvOrDefault("OPENWEATHER_BASE_URL", "https://api.openweathermap.org/data/2.5"),
		VisualCrossingAPIKey:  os.Getenv("VISUAL_CROSSING_API_KEY"),
		VisualCrossingBaseURL: getEnvOrDefault("VISUAL_CROSSING_BASE_URL", "https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline"),
		OpenMeteoBaseURL:      getEnvOrDefault("OPEN_METEO_BASE_URL", "https://archive-api.open-meteo.com/v1/archive"),

		// Sports
		OddsAPIKey:  os.Getenv("ODDS_API_KEY"),
		ESPNBaseURL: getEnvOrDefault("ESPN_BASE_URL", "http://site.api.espn.com/apis/site/v2/sports"),

		// Crypto
		CoinGeckoBaseURL: getEnvOrDefault("COINGECKO_BASE_URL", "https://api.coingecko.com/api/v3"),

		// Bot Settings
		MinProfitThreshold: getEnvAsFloat("MIN_PROFIT_THRESHOLD", 0.05),
		MaxPositionSize:    getEnvAsFloat("MAX_POSITION_SIZE", 100.0),
		CheckInterval:      getEnvAsInt("CHECK_INTERVAL_SECONDS", 60),
		LogLevel:           getEnvOrDefault("LOG_LEVEL", "info"),
		DatabasePath:       getEnvOrDefault("DATABASE_PATH", "./data/trades.db"),

		// Circuit Breaker Settings
		DailyLossLimit:  getEnvAsFloat("DAILY_LOSS_LIMIT", 500.0),
		DailyTradeLimit: getEnvAsInt("DAILY_TRADE_LIMIT", 50),

		// Market Quality Filters
		MinLiquidity: getEnvAsFloat("MIN_LIQUIDITY", 1000.0),   // $1000 minimum liquidity
		MinVolume:    getEnvAsFloat("MIN_VOLUME", 500.0),       // $500 minimum volume
		MinMarketAge: getEnvAsInt("MIN_MARKET_AGE_HOURS", 24),  // 24 hours old minimum
		MaxSpread:    getEnvAsFloat("MAX_SPREAD", 0.10),        // 10% max spread

		// Parity Arbitrage Settings
		ParityEnabled:                getEnvAsBool("PARITY_ENABLED", true),
		ParityMinProfitThreshold:     getEnvAsFloat("PARITY_MIN_PROFIT_THRESHOLD", 0.03), // 3% minimum
		ParityMinSpread:              getEnvAsFloat("PARITY_MIN_SPREAD", 0.05),            // 5 cents
		ParityMaxPositionSize:        getEnvAsFloat("PARITY_MAX_POSITION_SIZE", 100.0),
		ParityScanIntervalSeconds:    getEnvAsInt("PARITY_SCAN_INTERVAL_SECONDS", 30),
		ParityMinLiquidity:           getEnvAsFloat("PARITY_MIN_LIQUIDITY", 500.0),
		ParityMinVolume24h:           getEnvAsFloat("PARITY_MIN_VOLUME_24H", 100.0),
		ParityMaxConcurrentPositions: getEnvAsInt("PARITY_MAX_CONCURRENT_POSITIONS", 5),

		// Notifications
		DiscordWebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
	}

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that required configuration is present
func (c *Config) Validate() error {
	required := map[string]string{
		"POLYMARKET_API_KEY":        c.PolymarketAPIKey,
		"POLYMARKET_API_SECRET":     c.PolymarketAPISecret,
		"POLYMARKET_API_PASSPHRASE": c.PolymarketAPIPassphrase,
		"POLYMARKET_PRIVATE_KEY":    c.PolymarketPrivateKey,
	}

	for name, value := range required {
		if value == "" {
			return fmt.Errorf("required environment variable %s is not set", name)
		}
	}

	return nil
}

// Helper functions
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvAsFloat(key string, defaultValue float64) float64 {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}
