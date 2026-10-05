// Package config loads application configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/caarlos0/env/v11"
)

// Config holds all runtime settings. Values come from env vars (see .env.example).
type Config struct {
	// Server
	Port       int    `env:"PORT" envDefault:"8080"`
	BindAddr   string `env:"BIND_ADDR" envDefault:"127.0.0.1"`
	AppEnv     string `env:"APP_ENV" envDefault:"dev"`
	CORSOrigin string `env:"CORS_ORIGIN" envDefault:"http://localhost:5173"`
	AppToken   string `env:"APP_TOKEN"`

	// LLM
	LLMProvider        string `env:"LLM_PROVIDER" envDefault:"gemini"`
	GeminiAPIKey       string `env:"GEMINI_API_KEY"`
	LLMModel           string `env:"LLM_MODEL"`
	LLMFallbackModel   string `env:"LLM_FALLBACK_MODEL"`
	LLMMaxRetries      int    `env:"LLM_MAX_RETRIES" envDefault:"3"`
	LLMMaxTokens       int    `env:"LLM_MAX_TOKENS" envDefault:"1024"`
	AgentMaxIterations int    `env:"AGENT_MAX_ITERATIONS" envDefault:"6"`

	// Market data
	MarketProvider  string  `env:"MARKET_PROVIDER" envDefault:"finnhub"`
	FinnhubAPIKey   string  `env:"FINNHUB_API_KEY"`
	FinnhubRPS      float64 `env:"FINNHUB_RPS" envDefault:"1"`
	HistoryProvider string  `env:"HISTORY_PROVIDER"`
	TwelveDataKey   string  `env:"TWELVEDATA_API_KEY"`
	TwelveDataRPM   float64 `env:"TWELVEDATA_RPM" envDefault:"8"`
	MockMarket      string  `env:"MOCK_MARKET" envDefault:"auto"`

	// Storage
	DBPath string `env:"DB_PATH" envDefault:"./data/stockchat.db"`

	// Alerts
	AlertsPollSeconds int    `env:"ALERTS_POLL_SECONDS" envDefault:"15"`
	AlertsAlwaysOn    bool   `env:"ALERTS_ALWAYS_ON" envDefault:"false"`
	DiscordWebhookURL string `env:"DISCORD_WEBHOOK_URL"`
}

// Load parses env vars into a Config and validates it.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse env: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks that required settings are present for the selected providers.
func (c Config) Validate() error {
	var errs []error
	if c.Port <= 0 || c.Port > 65535 {
		errs = append(errs, fmt.Errorf("PORT out of range: %d", c.Port))
	}
	switch c.AppEnv {
	case "dev", "prod":
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be dev or prod, got %q", c.AppEnv))
	}
	switch c.LLMProvider {
	case "fake":
	case "gemini":
		if c.GeminiAPIKey == "" {
			errs = append(errs, errors.New("GEMINI_API_KEY is required when LLM_PROVIDER=gemini"))
		}
		if c.LLMModel == "" {
			errs = append(errs, errors.New("LLM_MODEL is required when LLM_PROVIDER=gemini"))
		}
	default:
		errs = append(errs, fmt.Errorf("LLM_PROVIDER must be gemini or fake, got %q", c.LLMProvider))
	}
	switch c.MarketProvider {
	case "mock":
	case "finnhub":
		if c.FinnhubAPIKey == "" {
			errs = append(errs, errors.New("FINNHUB_API_KEY is required when MARKET_PROVIDER=finnhub"))
		}
		if c.FinnhubRPS <= 0 {
			errs = append(errs, errors.New("FINNHUB_RPS must be positive"))
		}
	default:
		errs = append(errs, fmt.Errorf("MARKET_PROVIDER must be finnhub or mock, got %q", c.MarketProvider))
	}
	switch c.MockMarket {
	case "auto", "open", "closed":
	default:
		errs = append(errs, fmt.Errorf("MOCK_MARKET must be auto, open, or closed, got %q", c.MockMarket))
	}
	switch c.HistoryProvider {
	case "", "mock":
	case "twelvedata":
		if c.TwelveDataKey == "" {
			errs = append(errs, errors.New("TWELVEDATA_API_KEY is required when HISTORY_PROVIDER=twelvedata"))
		}
	default:
		errs = append(errs, fmt.Errorf("HISTORY_PROVIDER must be empty, mock, or twelvedata, got %q", c.HistoryProvider))
	}
	if c.AgentMaxIterations < 1 {
		errs = append(errs, errors.New("AGENT_MAX_ITERATIONS must be >= 1"))
	}
	if c.LLMMaxTokens < 1 {
		errs = append(errs, errors.New("LLM_MAX_TOKENS must be >= 1"))
	}
	if c.AlertsPollSeconds < 1 {
		errs = append(errs, errors.New("ALERTS_POLL_SECONDS must be >= 1"))
	}
	return errors.Join(errs...)
}

// Addr returns the host:port the HTTP server listens on.
func (c Config) Addr() string {
	return net.JoinHostPort(c.BindAddr, strconv.Itoa(c.Port))
}

// IsProd reports whether the app runs in production mode.
func (c Config) IsProd() bool { return c.AppEnv == "prod" }
