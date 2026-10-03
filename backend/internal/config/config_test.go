package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaultsOffline(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "fake")
	t.Setenv("MARKET_PROVIDER", "mock")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "127.0.0.1:8080", cfg.Addr())
	assert.Equal(t, 6, cfg.AgentMaxIterations)
	assert.False(t, cfg.IsProd())
}

func TestValidate(t *testing.T) {
	base := Config{
		Port: 8080, AppEnv: "dev", LLMProvider: "fake", MarketProvider: "mock", MockMarket: "auto",
		AgentMaxIterations: 6, LLMMaxTokens: 1024, AlertsPollSeconds: 15, FinnhubRPS: 1,
	}
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"valid offline", func(*Config) {}, ""},
		{"gemini needs key", func(c *Config) { c.LLMProvider = "gemini"; c.LLMModel = "m" }, "GEMINI_API_KEY"},
		{"gemini needs model", func(c *Config) { c.LLMProvider = "gemini"; c.GeminiAPIKey = "k" }, "LLM_MODEL"},
		{"finnhub needs key", func(c *Config) { c.MarketProvider = "finnhub" }, "FINNHUB_API_KEY"},
		{"unknown llm", func(c *Config) { c.LLMProvider = "x" }, "LLM_PROVIDER"},
		{"unknown market", func(c *Config) { c.MarketProvider = "x" }, "MARKET_PROVIDER"},
		{"twelvedata needs key", func(c *Config) { c.HistoryProvider = "twelvedata" }, "TWELVEDATA_API_KEY"},
		{"bad env", func(c *Config) { c.AppEnv = "staging" }, "APP_ENV"},
		{"bad port", func(c *Config) { c.Port = 0 }, "PORT"},
		{"bad iterations", func(c *Config) { c.AgentMaxIterations = 0 }, "AGENT_MAX_ITERATIONS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			tt.mutate(&c)
			err := c.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
