import type { ActionStatus, ChatEvent, ConfirmData, StoredMessage, UIBlock } from '../lib/types';

export interface TextPart {
  kind: 'text';
  text: string;
}

export interface ToolPart {
  kind: 'tool';
  callId: string;
  name: string;
  label: string;
  status: 'pending' | 'ok' | 'error';
  error?: string;
  ui?: UIBlock;
}

export interface ConfirmPart {
  kind: 'confirm';
  confirm: ConfirmData;
}

export type Part = TextPart | ToolPart | ConfirmPart;

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  parts: Part[];
  status: 'streaming' | 'done' | 'error' | 'stopped';
  error?: { message: string; retryable: boolean };
  /** For assistant messages: the user prompt that produced it (used by Retry). */
  prompt?: string;
}

export interface ChatState {
  conversationId: string | null;
  messages: ChatMessage[];
  streaming: boolean;
}

export const initialChatState: ChatState = { conversationId: null, messages: [], streaming: false };

export type ChatAction =
  | { type: 'reset' }
  | { type: 'load'; conversationId: string; messages: StoredMessage[] }
  | { type: 'send'; text: string; id: string }
  | { type: 'event'; event: ChatEvent }
  | { type: 'stopped' }
  | { type: 'failed'; message: string }
  | { type: 'confirm_update'; actionId: string; status: ActionStatus; result?: string };

const SYSTEM_NOTE = '[system note';

function updateLastAssistant(state: ChatState, f: (m: ChatMessage) => ChatMessage): ChatState {
  const idx = state.messages.length - 1;
  const last = state.messages[idx];
  if (!last || last.role !== 'assistant') return state;
  const messages = state.messages.slice();
  messages[idx] = f(last);
  return { ...state, messages };
}

function applyEvent(state: ChatState, ev: ChatEvent): ChatState {
  switch (ev.event) {
    case 'conversation':
      return { ...state, conversationId: ev.data.conversation_id };
    case 'text_delta':
      return updateLastAssistant(state, (m) => {
        const parts = m.parts.slice();
        const last = parts[parts.length - 1];
        if (last?.kind === 'text') parts[parts.length - 1] = { ...last, text: last.text + ev.data.text };
        else parts.push({ kind: 'text', text: ev.data.text });
        return { ...m, parts };
      });
    case 'tool_start':
      return updateLastAssistant(state, (m) => ({
        ...m,
        parts: [
          ...m.parts,
          {
            kind: 'tool',
            callId: ev.data.call_id,
            name: ev.data.name,
            label: ev.data.label,
            status: 'pending',
          },
        ],
      }));
    case 'tool_result':
      return updateLastAssistant(state, (m) => ({
        ...m,
        parts: m.parts.map((p) =>
          p.kind === 'tool' && p.callId === ev.data.call_id
            ? {
                ...p,
                status: ev.data.ok ? 'ok' : 'error',
                error: ev.data.error,
                ui: ev.data.ui,
              }
            : p,
        ),
      }));
    case 'confirmation_required':
      return updateLastAssistant(state, (m) => ({
        ...m,
        parts: insertAfterTool(m.parts, ev.data.call_id, { kind: 'confirm', confirm: ev.data }),
      }));
    case 'error':
      return {
        ...updateLastAssistant(state, (m) => ({
          ...m,
          status: 'error',
          error: { message: ev.data.message, retryable: ev.data.retryable },
          parts: settlePending(m.parts),
        })),
        streaming: false,
      };
    case 'done':
      return {
        ...updateLastAssistant(state, (m) => ({ ...m, status: 'done', parts: settlePending(m.parts) })),
        streaming: false,
      };
  }
}

function insertAfterTool(parts: Part[], callId: string, part: Part): Part[] {
  const idx = parts.findIndex((p) => p.kind === 'tool' && p.callId === callId);
  if (idx < 0) return [...parts, part];
  return [...parts.slice(0, idx + 1), part, ...parts.slice(idx + 1)];
}

/** Mark tool chips that never got a result as failed once the turn ends. */
function settlePending(parts: Part[]): Part[] {
  return parts.map((p) =>
    p.kind === 'tool' && p.status === 'pending' ? { ...p, status: 'error', error: 'Interrupted' } : p,
  );
}

export function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case 'reset':
      return initialChatState;
    case 'load':
      return {
        conversationId: action.conversationId,
        messages: fromStored(action.messages),
        streaming: false,
      };
    case 'send':
      return {
        ...state,
        streaming: true,
        messages: [
          ...state.messages,
          {
            id: `${action.id}-u`,
            role: 'user',
            parts: [{ kind: 'text', text: action.text }],
            status: 'done',
          },
          {
            id: `${action.id}-a`,
            role: 'assistant',
            parts: [],
            status: 'streaming',
            prompt: action.text,
          },
        ],
      };
    case 'event':
      return applyEvent(state, action.event);
    case 'stopped':
      return {
        ...updateLastAssistant(state, (m) =>
          m.status === 'streaming' ? { ...m, status: 'stopped', parts: settlePending(m.parts) } : m,
        ),
        streaming: false,
      };
    case 'failed':
      return {
        ...updateLastAssistant(state, (m) => ({
          ...m,
          status: 'error',
          error: { message: action.message, retryable: true },
          parts: settlePending(m.parts),
        })),
        streaming: false,
      };
    case 'confirm_update':
      return {
        ...state,
        messages: state.messages.map((m) => ({
          ...m,
          parts: m.parts.map((p) =>
            p.kind === 'confirm' && p.confirm.action_id === action.actionId
              ? { ...p, confirm: { ...p.confirm, status: action.status, result: action.result } }
              : p,
          ),
        })),
      };
  }
}

/**
 * Rebuild display messages from stored history. A user prompt starts a turn;
 * every following assistant message and tool-results message is folded into
 * one assistant bubble so text, chips, and cards keep their original order.
 */
export function fromStored(stored: StoredMessage[]): ChatMessage[] {
  const out: ChatMessage[] = [];
  let current: ChatMessage | null = null;
  let lastPrompt = '';

  for (const m of stored) {
    const isToolResults = m.blocks.some((b) => b.type === 'tool_result');
    if (m.role === 'user' && !isToolResults) {
      const text = m.blocks
        .filter((b) => b.type === 'text')
        .map((b) => b.text ?? '')
        .join('');
      if (text.startsWith(SYSTEM_NOTE)) continue; // internal confirmation notes
      lastPrompt = text;
      current = null;
      out.push({ id: `m${m.id}`, role: 'user', parts: [{ kind: 'text', text }], status: 'done' });
      continue;
    }
    if (!current) {
      current = { id: `m${m.id}`, role: 'assistant', parts: [], status: 'done', prompt: lastPrompt };
      out.push(current);
    }
    if (m.role === 'assistant') {
      for (const b of m.blocks) {
        if (b.type === 'text' && b.text) {
          current.parts.push({ kind: 'text', text: b.text });
        } else if (b.type === 'tool_use' && b.tool_use_id) {
          current.parts.push({
            kind: 'tool',
            callId: b.tool_use_id,
            name: b.tool_name ?? '',
            label: b.tool_name ?? '',
            status: 'pending',
          });
        }
      }
    } else {
      for (const entry of m.ui ?? []) {
        current.parts = current.parts.map((p) =>
          p.kind === 'tool' && p.callId === entry.call_id
            ? {
                ...p,
                label: entry.label || p.label,
                status: entry.ok ? 'ok' : 'error',
                error: entry.error,
                ui: entry.ui,
              }
            : p,
        );
        if (entry.confirm) {
          current.parts = insertAfterTool(current.parts, entry.call_id, {
            kind: 'confirm',
            confirm: entry.confirm,
          });
        }
      }
    }
  }
  for (const m of out) m.parts = settlePending(m.parts);
  return out;
}
