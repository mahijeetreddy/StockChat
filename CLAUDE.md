# StockChat: project rules
- Read PLAN.md before starting work. Follow milestones in order; tick checkboxes as completed.
- Backend: Go, chi, SQLite (modernc), slog. Frontend: React 18 + TS strict + Vite + Tailwind.
- Wrap errors with %w and context. Pass context.Context everywhere. No package-level globals for state.
- Table-driven tests; run `make test` and `make lint` before finishing a task.
- Never commit secrets. Never log API keys.
- LLM output is untrusted: validate all tool inputs; mutating actions require user confirmation.
- Numbers shown in the UI come from Go-side tool results, not from LLM text.
- Verify external API details (Finnhub, Gemini API / Go GenAI SDK) against current docs; log deviations in PLAN.md Decision log.
- Commit per milestone using conventional commits.
