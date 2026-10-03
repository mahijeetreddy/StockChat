// Types mirroring the Go backend (hand-written; keep in sync with
// backend/internal/domain, internal/tools, internal/agent/events.go).

export interface Quote {
  symbol: string;
  price: number;
  change: number;
  change_percent: number;
  high: number;
  low: number;
  open: number;
  prev_close: number;
  as_of: string;
  market_open: boolean;
}

export interface Candle {
  time: string;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

export type HistoryRange = '1D' | '5D' | '1M' | '3M' | '6M' | '1Y' | '5Y';

export interface PriceChartData {
  symbol: string;
  range: HistoryRange;
  intraday: boolean;
  candles: Candle[];
  start: number;
  end: number;
  change: number;
  change_percent: number;
  high: number;
  low: number;
}

export interface CompareSeries {
  symbol: string;
  start: number;
  end: number;
  change_percent: number;
  high: number;
  low: number;
  points: { time: string; pct: number }[];
}

export interface CompareData {
  range: HistoryRange;
  series: CompareSeries[];
}

export interface NewsItem {
  headline: string;
  summary: string;
  source: string;
  url: string;
  published_at: string;
}

export interface NewsListData {
  symbol: string;
  items: NewsItem[];
}

export interface CompanyProfile {
  symbol: string;
  name: string;
  exchange: string;
  industry: string;
  country: string;
  currency: string;
  web_url: string;
  logo_url: string;
  market_cap: number;
}

export type AlertKind = 'price_above' | 'price_below' | 'percent_change_day';

export interface Alert {
  id: number;
  symbol: string;
  kind: AlertKind;
  threshold: number;
  repeat: boolean;
  cooldown_minutes: number;
  status: 'active' | 'triggered' | 'deleted';
  created_at: string;
  last_fired_at?: string;
  description: string;
}

export interface AlertListData {
  alerts: Alert[];
}

export interface QuoteListData {
  quotes: Quote[];
  missing?: string[];
}

export type UIBlock =
  | { type: 'quote_card'; data: Quote }
  | { type: 'quote_list'; data: QuoteListData }
  | { type: 'price_chart'; data: PriceChartData }
  | { type: 'compare'; data: CompareData }
  | { type: 'news_list'; data: NewsListData }
  | { type: 'profile_card'; data: CompanyProfile }
  | { type: 'alert_list'; data: AlertListData };

export type ActionStatus = 'pending' | 'executing' | 'done' | 'failed' | 'cancelled' | 'expired';

export interface ConfirmData {
  action_id: string;
  call_id: string;
  summary: string;
  tool: string;
  input: unknown;
  status: ActionStatus;
  result?: string;
}

// ---- SSE events from POST /api/chat ----

export type ChatEvent =
  | { event: 'conversation'; data: { conversation_id: string } }
  | { event: 'text_delta'; data: { text: string } }
  | { event: 'tool_start'; data: { call_id: string; name: string; label: string } }
  | {
      event: 'tool_result';
      data: { call_id: string; name: string; ok: boolean; error?: string; ui?: UIBlock };
    }
  | { event: 'confirmation_required'; data: ConfirmData }
  | { event: 'error'; data: { message: string; retryable: boolean } }
  | {
      event: 'done';
      data: { message_id: string; usage: { input_tokens: number; output_tokens: number } };
    };

// ---- REST payloads ----

export interface Conversation {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface StoredBlock {
  type: 'text' | 'tool_use' | 'tool_result';
  text?: string;
  tool_use_id?: string;
  tool_name?: string;
  is_error?: boolean;
}

export interface UIEntry {
  call_id: string;
  name: string;
  label?: string;
  ok: boolean;
  error?: string;
  ui?: UIBlock;
  confirm?: ConfirmData;
}

export interface StoredMessage {
  id: number;
  role: 'user' | 'assistant';
  blocks: StoredBlock[];
  ui?: UIEntry[];
  created_at: string;
}

export interface ConversationDetail {
  conversation: Conversation;
  messages: StoredMessage[];
}

export interface MarketStatus {
  open: boolean;
  as_of: string;
  next_open?: string;
}

export interface WatchlistResponse {
  symbols: string[];
  quotes: Quote[];
}

export interface Notification {
  alert_id: number;
  symbol: string;
  kind: AlertKind;
  threshold: number;
  price: number;
  message: string;
  fired_at: string;
}
