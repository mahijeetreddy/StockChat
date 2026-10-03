package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mahijeetreddy/stockchat/backend/internal/domain"
	"github.com/mahijeetreddy/stockchat/backend/internal/market"
	"github.com/mahijeetreddy/stockchat/backend/internal/market/mock"
)

// Friday 2026-10-02 11:00 ET (open) and Saturday 2026-10-03 (closed).
var (
	friday   = time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)
	saturday = time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC)
)

func clock(t time.Time) func() time.Time { return func() time.Time { return t } }

func mockAt(t time.Time) *mock.Provider { return mock.New(mock.WithClock(clock(t))) }

func TestNormalizeSymbol(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"aapl", "AAPL", true},
		{" $msft ", "MSFT", true},
		{"BRK.B", "BRK.B", true},
		{"BF-B", "BF-B", true},
		{"", "", false},
		{"AAPL; DROP TABLE", "", false},
		{"TOOLONGTICKER", "", false},
		{"A1", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NormalizeSymbol(tt.in)
			if !tt.ok {
				var ie *InputError
				assert.True(t, errors.As(err, &ie))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDecodeStrict(t *testing.T) {
	var in symbolInput
	assert.NoError(t, decodeStrict(json.RawMessage(`{"symbol":"AAPL"}`), &in))
	assert.Error(t, decodeStrict(json.RawMessage(`{"symbol":"AAPL","extra":1}`), &in), "unknown fields rejected")
	assert.Error(t, decodeStrict(json.RawMessage(`{"symbol":1}`), &in), "wrong type rejected")
	assert.Error(t, decodeStrict(json.RawMessage(`{"symbol":"A"} {}`), &in), "trailing data rejected")
	assert.NoError(t, decodeStrict(nil, &in), "empty input is {}")
}

func TestGetQuote(t *testing.T) {
	tool := &GetQuote{Market: mockAt(friday), Now: clock(friday)}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"symbol":"aapl"}`))
	require.NoError(t, err)

	var fm quoteForModel
	require.NoError(t, json.Unmarshal([]byte(res.ForModel), &fm))
	assert.Equal(t, "AAPL", fm.Symbol)
	assert.True(t, fm.MarketOpen)
	assert.Empty(t, fm.Note)

	require.NotNil(t, res.UI)
	assert.Equal(t, UIQuoteCard, res.UI.Type)
	var q domain.Quote
	require.NoError(t, json.Unmarshal(res.UI.Data, &q))
	assert.Equal(t, fm.Price, q.Price, "UI and model see the same number")
	assert.Equal(t, "Fetching AAPL quote…", tool.Label(json.RawMessage(`{"symbol":"aapl"}`)))
}

func TestGetQuoteMarketClosed(t *testing.T) {
	tool := &GetQuote{Market: mockAt(saturday), Now: clock(saturday)}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"symbol":"AAPL"}`))
	require.NoError(t, err)
	var fm quoteForModel
	require.NoError(t, json.Unmarshal([]byte(res.ForModel), &fm))
	assert.False(t, fm.MarketOpen)
	assert.Contains(t, fm.Note, "closed")
	assert.Contains(t, fm.AsOf, "2026-10-02 16:00")
}

func TestGetQuoteErrors(t *testing.T) {
	tool := &GetQuote{Market: mockAt(friday), Now: clock(friday)}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"symbol":"ZZZZ"}`))
	require.ErrorIs(t, err, market.ErrNotFound)
	assert.Contains(t, SafeMessage(err), "not found")

	_, err = tool.Execute(context.Background(), json.RawMessage(`{"ticker":"AAPL"}`))
	var ie *InputError
	require.ErrorAs(t, err, &ie)
}

func TestSearchSymbol(t *testing.T) {
	tool := &SearchSymbol{Market: mockAt(friday)}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"Apple"}`))
	require.NoError(t, err)
	assert.Contains(t, res.ForModel, `"symbol":"AAPL"`)
	assert.Nil(t, res.UI)

	res, err = tool.Execute(context.Background(), json.RawMessage(`{"query":"asdfgh"}`))
	require.NoError(t, err)
	assert.Contains(t, res.ForModel, `"matches":[]`)
	assert.Contains(t, res.ForModel, "clarify")

	_, err = tool.Execute(context.Background(), json.RawMessage(`{"query":"  "}`))
	assert.Error(t, err)
}

func TestSafeMessageNeverLeaksRaw(t *testing.T) {
	raw := errors.New("Get \"https://finnhub.io/api/v1/quote?symbol=X\": dial tcp: secret-key-abc")
	msg := SafeMessage(raw)
	assert.NotContains(t, msg, "finnhub")
	assert.NotContains(t, msg, "secret")
}

func TestRegistry(t *testing.T) {
	r := NewRegistry(&GetQuote{}, &SearchSymbol{})
	specs := r.Specs()
	require.Len(t, specs, 2)
	assert.Equal(t, "get_quote", specs[0].Name)
	for _, s := range specs {
		assert.True(t, json.Valid(s.InputSchema), s.Name)
	}
	_, ok := r.Get("search_symbol")
	assert.True(t, ok)
	assert.Equal(t, "Running list alerts…", r.Label("list_alerts", nil))
	assert.Panics(t, func() { NewRegistry(&GetQuote{}, &GetQuote{}) })
}
