import { readSSE } from './sse';
import type {
  ActionStatus,
  Alert,
  ChatEvent,
  Conversation,
  ConversationDetail,
  HistoryRange,
  MarketStatus,
  PriceChartData,
  WatchlistResponse,
} from './types';

const TOKEN_KEY = 'stockchat.token';

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly body?: unknown,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? '';
  } catch {
    return '';
  }
}

export function setToken(token: string): void {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    // storage unavailable; token lasts for this page only
  }
}

function headers(extra?: HeadersInit): Headers {
  const h = new Headers(extra);
  const token = getToken();
  if (token) h.set('Authorization', `Bearer ${token}`);
  return h;
}

/** Fired when the server rejects the bearer token (APP_TOKEN is set). */
export const AUTH_REQUIRED_EVENT = 'stockchat:auth-required';

async function errorFrom(res: Response): Promise<ApiError> {
  if (res.status === 401) window.dispatchEvent(new Event(AUTH_REQUIRED_EVENT));
  let msg = `Request failed (${res.status})`;
  let body: unknown;
  try {
    body = await res.json();
    if (body && typeof body === 'object' && 'error' in body && typeof body.error === 'string') {
      msg = body.error;
    }
  } catch {
    // non-JSON error body
  }
  return new ApiError(res.status, msg, body);
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, { ...init, headers: headers(init?.headers) });
  if (!res.ok) throw await errorFrom(res);
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

const CHAT_EVENTS = new Set([
  'conversation',
  'text_delta',
  'tool_start',
  'tool_result',
  'confirmation_required',
  'error',
  'done',
]);

export interface StreamChatOptions {
  conversationId: string | null;
  message: string;
  signal: AbortSignal;
  onEvent: (event: ChatEvent) => void;
}

/** POST /api/chat and dispatch each SSE event as it arrives. */
export async function streamChat({
  conversationId,
  message,
  signal,
  onEvent,
}: StreamChatOptions): Promise<void> {
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const res = await fetch('/api/chat', {
    method: 'POST',
    headers: headers({ 'Content-Type': 'application/json', Accept: 'text/event-stream' }),
    body: JSON.stringify({ conversation_id: conversationId ?? undefined, message, timezone }),
    signal,
  });
  if (!res.ok) throw await errorFrom(res);
  if (!res.body) throw new ApiError(0, 'Streaming is not supported by this browser');
  await readSSE(res.body, (msg) => {
    if (!CHAT_EVENTS.has(msg.event)) return;
    let data: unknown;
    try {
      data = JSON.parse(msg.data);
    } catch {
      return;
    }
    onEvent({ event: msg.event, data } as ChatEvent);
  });
}

export interface ActionResult {
  action_id: string;
  status: ActionStatus;
  result: string;
}

export const api = {
  listConversations: () =>
    request<{ conversations: Conversation[] }>('/api/conversations').then((r) => r.conversations),
  getConversation: (id: string) =>
    request<ConversationDetail>(`/api/conversations/${encodeURIComponent(id)}`),
  deleteConversation: (id: string) =>
    request<undefined>(`/api/conversations/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  marketStatus: () => request<MarketStatus>('/api/market/status'),
  history: (symbol: string, range: HistoryRange) =>
    request<PriceChartData>(
      `/api/history/${encodeURIComponent(symbol)}?range=${encodeURIComponent(range)}`,
    ),
  watchlist: () => request<WatchlistResponse>('/api/watchlist'),
  alerts: () => request<{ alerts: Alert[] }>('/api/alerts').then((r) => r.alerts),
  deleteAlert: (id: number) => request<undefined>(`/api/alerts/${id}`, { method: 'DELETE' }),
  confirmAction: (id: string) =>
    request<ActionResult>(`/api/actions/${encodeURIComponent(id)}/confirm`, { method: 'POST' }),
  cancelAction: (id: string) =>
    request<ActionResult>(`/api/actions/${encodeURIComponent(id)}/cancel`, { method: 'POST' }),
};
