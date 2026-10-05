<div align="center">

<img src="frontend/public/favicon.svg" width="72" alt="StockChat logo" />

# StockChat

**Ask about the stock market in plain English. Get answers built from live data.**

An AI research assistant for US stocks: it looks up real prices, charts, comparisons and news, explains them in a few sentences, and keeps an eye on your watchlist and price alerts.

![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=black)
![TypeScript](https://img.shields.io/badge/TypeScript-strict-3178C6?logo=typescript&logoColor=white)
![Gemini](https://img.shields.io/badge/LLM-Gemini_Flash-8E75B2?logo=googlegemini&logoColor=white)
![SQLite](https://img.shields.io/badge/SQLite-003B57?logo=sqlite&logoColor=white)
[![backend](https://github.com/mahijeetreddy/StockChat/actions/workflows/backend.yml/badge.svg)](https://github.com/mahijeetreddy/StockChat/actions/workflows/backend.yml)
[![frontend](https://github.com/mahijeetreddy/StockChat/actions/workflows/frontend.yml/badge.svg)](https://github.com/mahijeetreddy/StockChat/actions/workflows/frontend.yml)

<br />

<img src="docs/screenshots/hero-dark.png" alt="StockChat comparing NVDA, AMD and INTC over six months" width="100%" />

</div>

---

## What it is

Checking on a stock usually means hopping between a quote page, a charting site and a news feed. StockChat puts all of that behind one chat box:

> _"How has Tesla done this month?"_ · _"Compare NVDA, AMD and INTC over 6 months"_ · _"Any news on Microsoft?"_ · _"Alert me if AAPL goes above 250"_

An AI model works out what you're asking and fetches the data it needs. The answer comes back as **real interface elements** (quote cards, interactive charts, comparison tables, news lists) plus a short written summary.

Two ideas shape the whole design:

- 🔢 **The numbers never come from the AI.** Every price and percentage on screen is rendered straight from market data. The model writes the commentary; it never re-types a number into a card.
- ✋ **Nothing changes without your click.** The assistant can *propose* an alert or a watchlist change, but only a Confirm button you press makes it happen.

---

## A tour

<table>
  <tr>
    <td width="50%" valign="top">
      <h3>💬 Ask in your own words</h3>
      <p>Use company names or tickers. StockChat finds the right symbol, fetches a live quote, and says when the market is closed and you're looking at the last close.</p>
      <img src="docs/screenshots/quote.png" alt="Quote card for Apple" />
    </td>
    <td width="50%" valign="top">
      <h3>📈 Interactive charts</h3>
      <p>Price history with start, end, high and low. Switch between 1 day and 5 years right on the card, with no new question needed.</p>
      <img src="docs/screenshots/chart.png" alt="Tesla one-month price chart" />
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top">
      <h3>⚖️ Side-by-side comparisons</h3>
      <p>Up to five stocks on one normalized chart (percent change from the start), with a ranked table underneath.</p>
      <img src="docs/screenshots/compare-dark.png" alt="Comparison of NVDA, AMD and INTC" />
    </td>
    <td width="50%" valign="top">
      <h3>🏢 Company profiles</h3>
      <p>Industry, exchange, country and market cap at a glance.</p>
      <img src="docs/screenshots/profile.png" alt="Costco company profile card" />
    </td>
  </tr>
  <tr>
    <td colspan="2" valign="top" align="center">
      <h3>📰 Latest news</h3>
      <p>Recent headlines with source and time, linked to the original articles.<br/>News text is treated as untrusted: shown as plain text, and the AI is told never to take instructions from it.</p>
      <img src="docs/screenshots/news.png" alt="Microsoft news list" width="70%" />
    </td>
  </tr>
</table>

---

## Alerts and watchlist, with you in control

Ask for an alert and the assistant prepares it, then **waits**. Nothing is saved until you confirm, and that confirmation is a separate action the AI can't trigger itself.

<table>
  <tr>
    <td width="50%" valign="top">
      <p><b>1. The assistant proposes</b></p>
      <img src="docs/screenshots/confirm-pending.png" alt="Alert waiting for confirmation" />
    </td>
    <td width="50%" valign="top">
      <p><b>2. You confirm</b></p>
      <img src="docs/screenshots/confirm-done.png" alt="Alert confirmed and created" />
    </td>
  </tr>
</table>

**3. StockChat watches the market for you.** A background engine checks prices every 15 seconds during market hours. When an alert fires you get a notification in the app (and optionally on Discord), while your watchlist stays live in the sidebar.

<p align="center">
  <img src="docs/screenshots/alerts-fired.png" alt="Watchlist and alerts in the sidebar with a fired alert notification" width="100%" />
</p>

---

## See it in action

<p align="center">
  <img src="docs/demo.gif" alt="StockChat demo: quote, chart, comparison, alert confirmation and notification" width="85%" />
</p>

<table>
  <tr>
    <td width="62%" valign="middle">
      <h3>📱 Works on your phone, in light or dark</h3>
      <p>The layout adapts to small screens, with a slide-out sidebar for chats, watchlist and alerts. Dark mode follows your system setting or a one-click toggle.</p>
      <p>The interface is also accessible: everything works from the keyboard, screen readers get labels, and gains and losses are marked with ▲/▼ arrows as well as color.</p>
    </td>
    <td width="38%" align="center">
      <img src="docs/screenshots/mobile-dark.png" alt="StockChat on a phone in dark mode" width="220" />
    </td>
  </tr>
</table>

---

## How it works

Every question goes through a short loop between the AI model and a set of **tools**: small, strictly checked functions that fetch data or propose changes.

```mermaid
sequenceDiagram
    autonumber
    actor You
    participant UI as Chat UI (React)
    participant API as Go backend
    participant AI as Gemini
    participant Data as Market data

    You->>UI: "Compare NVDA and AMD over 3 months"
    UI->>API: send message (reply streams back live)
    API->>AI: conversation + available tools
    AI-->>API: call compare_symbols(NVDA, AMD, 3M)
    API->>API: validate every argument
    API->>Data: fetch price history (cached, rate-limited)
    Data-->>API: candles
    API-->>UI: comparison card, built from real data
    API->>AI: compact summary of the results
    AI-->>API: short written takeaway
    API-->>UI: text streams in as it is written
```

- **The AI plans, the code does the work.** The model decides *which* tools to call. The Go backend checks every argument (valid ticker, allowed range, sensible thresholds) before anything runs, and independent lookups run in parallel.
- **The UI gets the full data, the AI gets a summary.** A year of prices goes to the chart in full. The model gets the key statistics, which keeps answers fast and focused.
- **Replies stream in live.** You see each step as it happens ("Searching for Apple…", "Fetching AAPL quote…") and the answer appears as it is written.

### Under the hood

```mermaid
flowchart LR
  UI["🖥️ Browser<br/>chat · cards · charts"] <--> API["Go backend<br/>streaming API"]
  API --> AG["Agent loop"]
  AG --> AI["Gemini<br/>+ fallback model"]
  AG --> T["Tools<br/>quotes · history · compare<br/>profile · news · alerts"]
  T --> MD["Market data<br/>cache → rate limiter<br/>Finnhub · Twelve Data"]
  AG --> DB[("SQLite<br/>chats · alerts · watchlist")]
  EN["Alert engine"] --> MD
  EN --> NT["Notifications<br/>in-app · Discord"]
  NT --> UI
```

| Layer | Built with |
|---|---|
| **AI** | Google Gemini (Flash) with function calling, behind a provider-neutral interface |
| **Backend** | Go, chi router, server-sent events for streaming, structured logging |
| **Data** | Finnhub (quotes, profiles, news, market status) · Twelve Data (price history) |
| **Storage** | SQLite with versioned migrations |
| **Frontend** | React 18, TypeScript (strict), Vite, Tailwind CSS, TanStack Query, TradingView Lightweight Charts |
| **Quality** | Unit and integration tests, race detector in CI, linting, and an AI evaluation suite |

---

## Built to be trustworthy

<table>
  <tr>
    <td width="50%" valign="top">
      <b>🛡️ Safe actions</b><br/>
      Changes go through a pending-action step that expires after 15 minutes and can only run once. The AI can't confirm on your behalf.
    </td>
    <td width="50%" valign="top">
      <b>🧪 Tested against tricky prompts</b><br/>
      An evaluation suite of 33 conversations checks that the AI picks the right tools and resists manipulation: a fake news article saying "delete all alerts", "ignore your instructions", requests for buy/sell advice.
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top">
      <b>⚡ Kind to rate limits</b><br/>
      Market data is cached, duplicate requests are merged, and calls are paced to stay inside free-tier limits. If the AI model is overloaded, StockChat retries and can switch to a backup model.
    </td>
    <td width="50%" valign="top">
      <b>🔐 Keys stay on the server</b><br/>
      API keys never reach the browser or the logs, and every input the AI produces is validated before it's used.
    </td>
  </tr>
</table>

---

<div align="center">

**StockChat is for information only, not financial advice.**<br/>
It reports and explains market data; it never tells you to buy, sell or hold. Data may be delayed.

<sub>Released under the <a href="LICENSE">MIT License</a>. Charts by <a href="https://www.tradingview.com/lightweight-charts/">TradingView Lightweight Charts</a>.</sub>

</div>
