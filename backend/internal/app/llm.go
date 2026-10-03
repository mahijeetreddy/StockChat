package app

import (
	"context"
	"fmt"

	"github.com/mahijeetreddy/stockchat/backend/internal/config"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm/fake"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm/gemini"
)

// NewLLM builds the configured LLM client.
func NewLLM(ctx context.Context, cfg config.Config) (llm.Client, error) {
	switch cfg.LLMProvider {
	case "gemini":
		return gemini.New(ctx, gemini.Config{APIKey: cfg.GeminiAPIKey, Model: cfg.LLMModel, MaxRetries: cfg.LLMMaxRetries})
	case "fake":
		return fake.NewDemo(), nil
	default:
		return nil, fmt.Errorf("unknown LLM_PROVIDER %q", cfg.LLMProvider)
	}
}
