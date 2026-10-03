package fake

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mahijeetreddy/stockchat/backend/internal/llm"
)

// NewDemo returns a rule-based offline "model" used when LLM_PROVIDER=fake.
// It picks tools from keywords and writes short summaries from tool results,
// so the whole app can be demoed with zero API keys. It is not an LLM: phrasing
// outside its simple patterns gets a request to rephrase.
func NewDemo() *Client {
	return NewFunc(demoRespond)
}

var (
	tickerRe  = regexp.MustCompile(`\$?\b[A-Z]{1,5}(?:\.[A-Z])?\b`)
	numberRe  = regexp.MustCompile(`\$?(\d+(?:\.\d+)?)\s*(%|percent)?`)
	alertIDRe = regexp.MustCompile(`(?i)alert\s*(?:#|id\s*|number\s*)?(\d+)`)
	notTicker = map[string]bool{
		"I": true, "A": true, "AND": true, "OR": true, "ME": true, "MY": true, "US": true, "THE": true,
		"IF": true, "IS": true, "IT": true, "TO": true, "OF": true, "ON": true, "IN": true, "AT": true,
		"PM": true, "AM": true, "ET": true, "OK": true, "VS": true, "CEO": true, "ETF": true, "USD": true,
		"AI": true, "PE": true, "EPS": true, "YTD": true, "IPO": true, "NEWS": true, "HI": true, "SO": true,
	}
	fillerWords = map[string]bool{
		"what": true, "what's": true, "whats": true, "is": true, "are": true, "the": true, "price": true, "of": true,
		"trading": true, "at": true, "how": true, "has": true, "have": true, "done": true, "doing": true, "this": true,
		"month": true, "week": true, "year": true, "today": true, "tell": true, "me": true, "about": true, "any": true,
		"news": true, "on": true, "for": true, "stock": true, "stocks": true, "shares": true, "share": true, "a": true,
		"quote": true, "right": true, "now": true, "current": true, "show": true, "give": true, "company": true,
		"profile": true, "headlines": true, "latest": true, "please": true, "can": true, "you": true, "i": true,
		"chart": true, "history": true, "performance": true, "performed": true, "perform": true, "did": true,
		"over": true, "last": true, "past": true, "months": true, "years": true, "days": true, "and": true,
		"in": true, "to": true, "my": true, "watchlist": true, "add": true, "an": true, "it": true, "up": true, "lately": true,
		"should": true, "buy": true, "sell": true, "hold": true, "do": true, "does": true, "alert": true, "alerts": true,
		"if": true, "goes": true, "above": true, "below": true, "notify": true, "remove": true, "delete": true,
		"from": true, "compare": true, "vs": true, "versus": true, "with": true, "when": true, "who": true,
	}
)

type intent int

const (
	intentQuote intent = iota
	intentHistory
	intentCompare
	intentNews
	intentProfile
	intentAlertCreate
	intentAlertList
	intentAlertDelete
	intentWatchAdd
	intentWatchRemove
	intentWatchList
	intentAdvice
)

type plan struct {
	intent  intent
	symbols []string
	query   string // company name to search when no ticker was given
	rng     string
	kind    string
	thresh  float64
	alertID int64
}

func parsePlan(text string) plan {
	t := strings.ToLower(text)
	p := plan{rng: parseRange(t)}

	for _, m := range tickerRe.FindAllString(text, -1) {
		s := strings.TrimPrefix(m, "$")
		if !notTicker[s] && !contains(p.symbols, s) {
			p.symbols = append(p.symbols, s)
		}
	}
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(t, w) {
				return true
			}
		}
		return false
	}

	switch {
	case has("should i buy", "should i sell", "buy or sell", "good investment", "should i invest", "worth buying", "should i hold"):
		p.intent = intentAdvice
	case has("watchlist", "watch list") && has("remove", "delete", "drop"):
		p.intent = intentWatchRemove
	case has("watchlist", "watch list") && has("add", "put", "track"):
		p.intent = intentWatchAdd
	case has("watchlist", "watch list"):
		p.intent = intentWatchList
	case has("alert") && has("delete", "remove", "cancel"):
		p.intent = intentAlertDelete
		if m := alertIDRe.FindStringSubmatch(text); m != nil {
			p.alertID, _ = strconv.ParseInt(m[1], 10, 64)
		}
	case has("what alerts", "my alerts", "list alerts", "show alerts", "alerts do i", "which alerts", "active alerts"):
		p.intent = intentAlertList
	case has("alert", "notify", "let me know") && has("above", "over", "below", "under", "drop", "fall", "rise", "goes", "%", "percent", "hits", "reaches"):
		p.intent = intentAlertCreate
		p.kind, p.thresh = parseAlert(t)
	case has("news", "headline"):
		p.intent = intentNews
	case has("compare", " vs", "versus") || (len(p.symbols) > 1 && p.rng != ""):
		p.intent = intentCompare
	case has("tell me about", "profile", "what does", "who is", "what is the company", "company info"):
		p.intent = intentProfile
	case p.rng != "" || has("chart", "history", "performance", "performed", "how has", "done"):
		p.intent = intentHistory
	default:
		p.intent = intentQuote
	}
	if p.intent == intentHistory || p.intent == intentCompare {
		if p.rng == "" {
			p.rng = "1M"
		}
	}
	switch p.intent {
	case intentAlertList, intentWatchList, intentAlertDelete, intentAdvice:
	default:
		if len(p.symbols) == 0 {
			p.query = extractQuery(text)
		}
	}
	return p
}

func parseRange(t string) string {
	switch {
	case strings.Contains(t, "5 year") || strings.Contains(t, "five year"):
		return "5Y"
	case strings.Contains(t, "6 month") || strings.Contains(t, "six month") || strings.Contains(t, "half"):
		return "6M"
	case strings.Contains(t, "3 month") || strings.Contains(t, "three month") || strings.Contains(t, "quarter"):
		return "3M"
	case strings.Contains(t, "year") || strings.Contains(t, "12 month"):
		return "1Y"
	case strings.Contains(t, "month"):
		return "1M"
	case strings.Contains(t, "week") || strings.Contains(t, "5 day") || strings.Contains(t, "five day"):
		return "5D"
	case strings.Contains(t, "intraday"):
		return "1D"
	}
	return ""
}

func parseAlert(t string) (string, float64) {
	var num float64
	isPct := false
	for _, m := range numberRe.FindAllStringSubmatch(t, -1) {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			num, isPct = v, m[2] != ""
			break
		}
	}
	switch {
	case isPct:
		return "percent_change_day", num
	case strings.Contains(t, "below") || strings.Contains(t, "under") || strings.Contains(t, "drop") || strings.Contains(t, "fall"):
		return "price_below", num
	default:
		return "price_above", num
	}
}

func extractQuery(text string) string {
	var words []string
	for _, w := range strings.Fields(text) {
		w = strings.Trim(w, "?!.,'\"")
		w = strings.TrimSuffix(strings.TrimSuffix(w, "'s"), "’s")
		if w == "" || fillerWords[strings.ToLower(w)] {
			continue
		}
		words = append(words, w)
		if len(words) == 3 {
			break
		}
	}
	return strings.Join(words, " ")
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// lastUserText returns the latest plain user message (not tool results or system notes).
func lastUserText(msgs []llm.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role == llm.RoleUser && !m.HasToolResults() && !strings.HasPrefix(m.Text(), "[system note") {
			return m.Text()
		}
	}
	return ""
}

func demoRespond(turn int, req llm.Request) Response {
	msgs := req.Messages
	if len(msgs) == 0 {
		return Response{Text: []string{"Hi! Ask me about a US stock."}}
	}
	last := msgs[len(msgs)-1]
	userText := lastUserText(msgs)
	p := parsePlan(userText)

	if last.HasToolResults() && len(msgs) >= 2 {
		calls := msgs[len(msgs)-2].ToolUses()
		if len(calls) > 0 && calls[0].ToolName == "search_symbol" {
			sym, reply := resolveSearch(last.Blocks[0], p.query)
			if reply != "" {
				return textResponse(reply)
			}
			p.symbols = []string{sym}
			return actOn(p)
		}
		return textResponse(summarize(calls, last.Blocks))
	}

	if p.intent == intentAdvice {
		return textResponse("I can't tell you whether to buy, sell, or hold a stock. I can show you its recent price history, latest news, or company profile so you can decide for yourself. This is not financial advice.")
	}
	switch p.intent {
	case intentAlertList:
		return withCalls(toolCall("list_alerts", map[string]any{}))
	case intentWatchList:
		return withCalls(toolCall("list_watchlist", map[string]any{}))
	case intentAlertDelete:
		if p.alertID == 0 {
			return textResponse("Which alert should I delete? Tell me its number (for example, \"delete alert 3\"). You can ask \"what alerts do I have?\" to see them.")
		}
		return withCalls(toolCall("delete_alert", map[string]any{"alert_id": p.alertID}))
	}
	if len(p.symbols) == 0 {
		if p.query == "" {
			return textResponse("Which stock do you mean? A ticker like AAPL works best.")
		}
		return withCalls(toolCall("search_symbol", map[string]any{"query": p.query}))
	}
	return actOn(p)
}

func toolCall(name string, input map[string]any) ToolCall { return ToolCall{Name: name, Input: input} }

func withCalls(tc ...ToolCall) Response { return Response{ToolCalls: tc} }

func textResponse(s string) Response {
	// Stream in a few chunks so the UI shows streaming.
	var parts []string
	words := strings.SplitAfter(s, " ")
	for i := 0; i < len(words); i += 4 {
		parts = append(parts, strings.Join(words[i:min(i+4, len(words))], ""))
	}
	return Response{Text: parts}
}

func actOn(p plan) Response {
	var tcs []ToolCall
	for _, s := range p.symbols {
		switch p.intent {
		case intentHistory, intentCompare:
			tcs = append(tcs, toolCall("get_history", map[string]any{"symbol": s, "range": p.rng}))
		case intentNews:
			tcs = append(tcs, toolCall("get_news", map[string]any{"symbol": s, "limit": 5}))
		case intentProfile:
			tcs = append(tcs, toolCall("get_company_profile", map[string]any{"symbol": s}))
		case intentAlertCreate:
			if p.thresh <= 0 {
				return textResponse(fmt.Sprintf("What price (or %% move) should trigger the %s alert?", s))
			}
			tcs = append(tcs, toolCall("create_alert", map[string]any{"symbol": s, "kind": p.kind, "threshold": p.thresh}))
		case intentWatchAdd:
			tcs = append(tcs, toolCall("watchlist_add", map[string]any{"symbol": s}))
		case intentWatchRemove:
			tcs = append(tcs, toolCall("watchlist_remove", map[string]any{"symbol": s}))
		default:
			tcs = append(tcs, toolCall("get_quote", map[string]any{"symbol": s}))
		}
		if p.intent == intentAlertCreate {
			break // one alert per message
		}
	}
	return withCalls(tcs...)
}

type searchResult struct {
	Matches []struct {
		Symbol      string `json:"symbol"`
		Description string `json:"description"`
	} `json:"matches"`
}

// resolveSearch picks a symbol from search results or returns a clarification.
func resolveSearch(b llm.ContentBlock, query string) (string, string) {
	var r searchResult
	if b.IsError || json.Unmarshal([]byte(b.Content), &r) != nil || len(r.Matches) == 0 {
		return "", fmt.Sprintf("I couldn't find a US-listed stock matching %q. Could you double-check the name or give me the ticker?", query)
	}
	q := strings.ToUpper(strings.TrimSpace(query))
	first := r.Matches[0]
	canonical := false
	for _, suf := range []string{" INC", " CORP", " CO", " CORPORATION", ".COM INC", " WHOLESALE CORP"} {
		if first.Description == q+suf {
			canonical = true
		}
	}
	if !canonical && len(r.Matches) > 1 && strings.HasPrefix(r.Matches[1].Description, q) && strings.HasPrefix(first.Description, q) {
		var opts []string
		for _, m := range r.Matches[:min(3, len(r.Matches))] {
			opts = append(opts, fmt.Sprintf("%s (%s)", m.Symbol, titleCase(m.Description)))
		}
		return "", fmt.Sprintf("I found more than one match for %q: %s. Which one do you mean?", query, strings.Join(opts, ", "))
	}
	return first.Symbol, ""
}

func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// summarize writes a short reply from tool results.
func summarize(calls []llm.ContentBlock, results []llm.ContentBlock) string {
	var parts []string
	for i, r := range results {
		name := r.ToolName
		if name == "" && i < len(calls) {
			name = calls[i].ToolName
		}
		if r.IsError {
			parts = append(parts, fmt.Sprintf("I couldn't complete %s: %s.", strings.ReplaceAll(name, "_", " "), r.Content))
			continue
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(r.Content), &m)
		if m["status"] == "pending_confirmation" {
			parts = append(parts, fmt.Sprintf("I've prepared this: %v. It isn't active yet; please click Confirm on the card to apply it.", m["summary"]))
			continue
		}
		switch name {
		case "get_quote":
			s := fmt.Sprintf("**%v** is at $%.2f (%+.2f, %+.2f%%).", m["symbol"], num(m["price"]), num(m["change"]), num(m["change_percent"]))
			if open, _ := m["market_open"].(bool); !open {
				s += fmt.Sprintf(" The market is closed, so that's the last close as of %v.", m["as_of"])
			}
			parts = append(parts, s)
		case "get_history":
			parts = append(parts, fmt.Sprintf("**%v** moved %+.2f%% over %v, ranging from $%.2f to $%.2f.", m["symbol"], num(m["change_percent"]), m["range"], num(m["low"]), num(m["high"])))
		case "get_news":
			parts = append(parts, fmt.Sprintf("Here are the latest headlines for **%v**. News text comes from third parties, so read the sources for details.", m["symbol"]))
		case "get_company_profile":
			parts = append(parts, fmt.Sprintf("**%v** (%v) is in the %v industry and trades on %v.", m["name"], m["symbol"], m["industry"], m["exchange"]))
		case "compare_symbols":
			parts = append(parts, "Here's the comparison. The chart shows each stock's % change from the start of the period.")
		case "list_alerts":
			n := 0
			if a, ok := m["alerts"].([]any); ok {
				n = len(a)
			}
			parts = append(parts, fmt.Sprintf("You have %d active alert(s).", n))
		case "list_watchlist":
			n := 0
			if a, ok := m["symbols"].([]any); ok {
				n = len(a)
			}
			parts = append(parts, fmt.Sprintf("Your watchlist has %d symbol(s).", n))
		default:
			parts = append(parts, "Done.")
		}
	}
	return strings.Join(parts, "\n\n")
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}
