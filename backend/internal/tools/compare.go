package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
)

// CompareSeries is one symbol in the compare UI payload.
type CompareSeries struct {
	Symbol        string         `json:"symbol"`
	Start         float64        `json:"start"`
	End           float64        `json:"end"`
	ChangePercent float64        `json:"change_percent"`
	High          float64        `json:"high"`
	Low           float64        `json:"low"`
	Points        []ComparePoint `json:"points"`
}

// ComparePoint is % change from the period start at a time.
type ComparePoint struct {
	Time string  `json:"time"`
	Pct  float64 `json:"pct"`
}

// Compare is the compare UI payload.
type Compare struct {
	Range  domain.HistoryRange `json:"range"`
	Series []CompareSeries     `json:"series"`
}

// CompareSymbols is the compare_symbols tool.
type CompareSymbols struct {
	Market market.Provider
}

type compareInput struct {
	Symbols []string `json:"symbols"`
	Range   string   `json:"range"`
}

// Spec implements Tool.
func (t *CompareSymbols) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "compare_symbols",
		Description: "Compare the performance of 2 to 5 US tickers over the same range: % change, start/end price, high and low for each. " +
			"Use for 'compare X and Y', 'X vs Y', 'which did better'. The UI shows a comparison table and a normalized % chart.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"symbols":{"type":"array","items":{"type":"string"},"minItems":2,"maxItems":5,"description":"US tickers, e.g. [\"NVDA\",\"AMD\"]"},` +
			`"range":{"type":"string","enum":` + rangeEnum + `,"description":"Lookback window; defaults to 1M"}` +
			`},"required":["symbols","range"],"additionalProperties":false}`),
	}
}

// Mutating implements Tool.
func (t *CompareSymbols) Mutating() bool { return false }

// Label implements Labeler.
func (t *CompareSymbols) Label(input json.RawMessage) string {
	var in compareInput
	_ = json.Unmarshal(input, &in)
	var syms []string
	for _, s := range in.Symbols {
		if s = strings.ToUpper(strings.TrimSpace(s)); symbolRe.MatchString(s) {
			syms = append(syms, s)
		}
	}
	if len(syms) >= 2 && len(syms) <= 5 {
		return fmt.Sprintf("Comparing %s…", strings.Join(syms, ", "))
	}
	return "Comparing stocks…"
}

// Execute implements Tool.
func (t *CompareSymbols) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var in compareInput
	if err := decodeStrict(input, &in); err != nil {
		return Result{}, err
	}
	if len(in.Symbols) < 2 || len(in.Symbols) > 5 {
		return Result{}, inputErr("symbols must list 2 to 5 tickers")
	}
	var syms []string
	seen := map[string]bool{}
	for _, s := range in.Symbols {
		n, err := NormalizeSymbol(s)
		if err != nil {
			return Result{}, err
		}
		if !seen[n] {
			seen[n] = true
			syms = append(syms, n)
		}
	}
	if len(syms) < 2 {
		return Result{}, inputErr("symbols must list 2 to 5 different tickers")
	}
	r, err := parseRange(in.Range)
	if err != nil {
		return Result{}, err
	}

	results := make([][]domain.Candle, len(syms))
	errs := make([]error, len(syms))
	var wg sync.WaitGroup
	for i, s := range syms {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = t.Market.History(ctx, s, r)
		}()
	}
	wg.Wait()

	ui := Compare{Range: r}
	type modelRow struct {
		Symbol        string  `json:"symbol"`
		Start         float64 `json:"start"`
		End           float64 `json:"end"`
		ChangePercent float64 `json:"change_percent"`
		High          float64 `json:"high"`
		Low           float64 `json:"low"`
	}
	var rows []modelRow
	failed := map[string]string{}
	var firstErr error
	for i, s := range syms {
		if errs[i] != nil || len(results[i]) == 0 {
			if errs[i] == nil {
				errs[i] = market.ErrNotFound
			}
			if firstErr == nil {
				firstErr = errs[i]
			}
			failed[s] = SafeMessage(errs[i])
			continue
		}
		c := results[i]
		st := Summarize(c)
		pts := make([]ComparePoint, len(c))
		for j, cd := range c {
			pct := 0.0
			if st.Start != 0 {
				pct = round((cd.Close-st.Start)/st.Start*100, 3)
			}
			pts[j] = ComparePoint{Time: cd.Time.UTC().Format("2006-01-02T15:04:05Z"), Pct: pct}
		}
		row := modelRow{Symbol: s, Start: round(st.Start, 2), End: round(st.End, 2), ChangePercent: round(st.ChangePercent, 2), High: round(st.High, 2), Low: round(st.Low, 2)}
		rows = append(rows, row)
		ui.Series = append(ui.Series, CompareSeries{Symbol: s, Start: row.Start, End: row.End, ChangePercent: row.ChangePercent, High: row.High, Low: row.Low, Points: pts})
	}
	if len(rows) == 0 {
		return Result{}, fmt.Errorf("compare_symbols: %w", firstErr)
	}
	out := map[string]any{"range": r, "results": rows}
	if len(failed) > 0 {
		out["failed"] = failed
	}
	fm, err := marshalForModel(out)
	if err != nil {
		return Result{}, err
	}
	block, err := NewUIBlock(UICompare, ui)
	if err != nil {
		return Result{}, err
	}
	return Result{ForModel: fm, UI: block}, nil
}
