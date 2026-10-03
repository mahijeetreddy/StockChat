package fake

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
)

func TestParsePlan(t *testing.T) {
	tests := []struct {
		text    string
		intent  intent
		symbols []string
		query   string
		rng     string
	}{
		{"What's Apple trading at?", intentQuote, nil, "Apple", ""},
		{"price of AAPL right now", intentQuote, []string{"AAPL"}, "", ""},
		{"How has Tesla done this month?", intentHistory, nil, "Tesla", "1M"},
		{"Compare NVDA, AMD, and INTC over 6 months", intentCompare, []string{"NVDA", "AMD", "INTC"}, "", "6M"},
		{"Any news on Microsoft?", intentNews, nil, "Microsoft", ""},
		{"Tell me about Costco", intentProfile, nil, "Costco", ""},
		{"Alert me if AAPL goes above 250", intentAlertCreate, []string{"AAPL"}, "", ""},
		{"What alerts do I have?", intentAlertList, nil, "", ""},
		{"Add TSLA and NVDA to my watchlist", intentWatchAdd, []string{"TSLA", "NVDA"}, "", ""},
		{"Should I buy Tesla?", intentAdvice, nil, "", ""},
		{"What's Apple's price?", intentQuote, nil, "Apple", ""},
		{"asdfgh price", intentQuote, nil, "asdfgh", ""},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			p := parsePlan(tt.text)
			assert.Equal(t, tt.intent, p.intent)
			assert.Equal(t, tt.symbols, p.symbols)
			assert.Equal(t, tt.query, p.query)
			if tt.rng != "" {
				assert.Equal(t, tt.rng, p.rng)
			}
		})
	}
}

func TestParseAlert(t *testing.T) {
	kind, n := parseAlert("alert me if nvda drops 5% today")
	assert.Equal(t, "percent_change_day", kind)
	assert.Equal(t, 5.0, n)
	kind, n = parseAlert("tell me if aapl falls below $180.50")
	assert.Equal(t, "price_below", kind)
	assert.Equal(t, 180.5, n)
}

func TestDemoSearchFlow(t *testing.T) {
	user := llm.TextMessage(llm.RoleUser, "What's Apple trading at?")
	r := demoRespond(0, llm.Request{Messages: []llm.Message{user}})
	assert.Equal(t, "search_symbol", r.ToolCalls[0].Name)

	call := llm.ContentBlock{Type: llm.BlockToolUse, ToolName: "search_symbol"}
	res := llm.ContentBlock{Type: llm.BlockToolResult, ToolName: "search_symbol",
		Content: `{"matches":[{"symbol":"AAPL","description":"APPLE INC"},{"symbol":"APLE","description":"APPLE HOSPITALITY REIT INC"}]}`}
	r = demoRespond(1, llm.Request{Messages: []llm.Message{user, {Role: llm.RoleAssistant, Blocks: []llm.ContentBlock{call}}, {Role: llm.RoleUser, Blocks: []llm.ContentBlock{res}}}})
	assert.Equal(t, "get_quote", r.ToolCalls[0].Name)
	assert.Equal(t, map[string]any{"symbol": "AAPL"}, r.ToolCalls[0].Input)

	// Ambiguous names get a clarifying question.
	user = llm.TextMessage(llm.RoleUser, "price of Delta")
	res.Content = `{"matches":[{"symbol":"DAL","description":"DELTA AIR LINES INC"},{"symbol":"DLA","description":"DELTA APPAREL INC"}]}`
	r = demoRespond(1, llm.Request{Messages: []llm.Message{user, {Role: llm.RoleAssistant, Blocks: []llm.ContentBlock{call}}, {Role: llm.RoleUser, Blocks: []llm.ContentBlock{res}}}})
	assert.Empty(t, r.ToolCalls)
	assert.Contains(t, joined(r.Text), "Which one do you mean?")
}

func joined(parts []string) string {
	s := ""
	for _, p := range parts {
		s += p
	}
	return s
}
