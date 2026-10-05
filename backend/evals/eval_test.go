//go:build eval

package evals

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mahijeetreddy/stockchat/backend/internal/config"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm/gemini"
)

// TestLiveEvals runs every case against the real Gemini API with the mock
// market provider. Requires GEMINI_API_KEY and LLM_MODEL (from env or .env).
//
//	make eval
//	EVAL_ONLY=advice_buy,compare_three make eval   # subset
//	EVAL_DELAY=6s make eval                        # slower, for strict free-tier RPM
func TestLiveEvals(t *testing.T) {
	_ = config.LoadDotEnv("../.env", "../../.env")
	key, model := os.Getenv("GEMINI_API_KEY"), os.Getenv("LLM_MODEL")
	if key == "" || model == "" {
		t.Skip("GEMINI_API_KEY and LLM_MODEL are required for live evals")
	}
	cases, err := Load("cases.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if only := os.Getenv("EVAL_ONLY"); only != "" {
		want := map[string]bool{}
		for _, id := range strings.Split(only, ",") {
			want[strings.TrimSpace(id)] = true
		}
		var filtered []Case
		for _, c := range cases {
			if want[c.ID] {
				filtered = append(filtered, c)
			}
		}
		cases = filtered
	}
	delay := 4 * time.Second // stay under free-tier requests-per-minute
	if d, err := time.ParseDuration(os.Getenv("EVAL_DELAY")); err == nil {
		delay = d
	}
	retries := 4
	if n, err := strconv.Atoi(os.Getenv("LLM_MAX_RETRIES")); err == nil {
		retries = n
	}

	ctx := context.Background()
	client, err := gemini.New(ctx, gemini.Config{
		APIKey: key, Model: model, FallbackModel: os.Getenv("LLM_FALLBACK_MODEL"),
		MaxRetries: retries, MaxBackoff: 60 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	var results []Result
	for i, c := range cases {
		if i > 0 {
			time.Sleep(delay)
		}
		start := time.Now()
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		obs := Run(cctx, client, c, dir)
		cancel()
		r := Result{Case: c, Observed: obs, Fails: Score(c, obs), Duration: time.Since(start)}
		results = append(results, r)
		status := "PASS"
		if !r.Passed() {
			status = "FAIL"
		}
		fmt.Printf("[%2d/%d] %-28s %s (%s)\n", i+1, len(cases), c.ID, status, r.Duration.Round(time.Second))
		if !r.Passed() {
			fmt.Printf("        why: %s\n", strings.Join(r.Fails, "; "))
		}
	}
	passed := Report(os.Stdout, results)
	if passed < len(results) {
		for _, r := range results {
			if !r.Passed() {
				fmt.Printf("\n--- %s\nprompt: %s\nanswer: %s\n", r.Case.ID, r.Case.Prompt, r.Observed.Answer)
			}
		}
		t.Errorf("%d/%d cases failed", len(results)-passed, len(results))
	}
}
