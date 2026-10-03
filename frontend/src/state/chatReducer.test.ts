import { chatReducer, fromStored, initialChatState, type ChatState } from './chatReducer';
import type { ChatEvent, StoredMessage } from '../lib/types';

const quote = {
  symbol: 'AAPL',
  price: 230,
  change: 1,
  change_percent: 0.4,
  high: 231,
  low: 228,
  open: 229,
  prev_close: 229,
  as_of: '2026-10-02T20:00:00Z',
  market_open: false,
};

function run(events: ChatEvent[], start: ChatState = initialChatState): ChatState {
  let s = chatReducer(start, { type: 'send', text: 'price of apple', id: 'x' });
  for (const event of events) s = chatReducer(s, { type: 'event', event });
  return s;
}

describe('chatReducer', () => {
  it('streams text, tool chips, and blocks in arrival order', () => {
    const s = run([
      { event: 'conversation', data: { conversation_id: 'c1' } },
      { event: 'text_delta', data: { text: 'Let me ' } },
      { event: 'text_delta', data: { text: 'check.' } },
      {
        event: 'tool_start',
        data: { call_id: 'a', name: 'get_quote', label: 'Fetching AAPL quote…' },
      },
      {
        event: 'tool_result',
        data: { call_id: 'a', name: 'get_quote', ok: true, ui: { type: 'quote_card', data: quote } },
      },
      { event: 'text_delta', data: { text: 'AAPL is $230.' } },
      { event: 'done', data: { message_id: '4', usage: { input_tokens: 1, output_tokens: 1 } } },
    ]);
    expect(s.conversationId).toBe('c1');
    expect(s.streaming).toBe(false);
    expect(s.messages).toHaveLength(2);
    const a = s.messages[1]!;
    expect(a.status).toBe('done');
    expect(a.prompt).toBe('price of apple');
    expect(a.parts.map((p) => p.kind)).toEqual(['text', 'tool', 'text']);
    expect(a.parts[0]).toEqual({ kind: 'text', text: 'Let me check.' });
    const tool = a.parts[1];
    expect(tool?.kind === 'tool' && tool.status).toBe('ok');
    expect(tool?.kind === 'tool' && tool.ui?.type).toBe('quote_card');
  });

  it('resolves parallel tool results by call id regardless of order', () => {
    const s = run([
      { event: 'tool_start', data: { call_id: 'a', name: 'get_history', label: 'A' } },
      { event: 'tool_start', data: { call_id: 'b', name: 'get_history', label: 'B' } },
      { event: 'tool_result', data: { call_id: 'b', name: 'get_history', ok: false, error: 'nope' } },
      { event: 'tool_result', data: { call_id: 'a', name: 'get_history', ok: true } },
    ]);
    const parts = s.messages[1]!.parts;
    expect(parts.map((p) => (p.kind === 'tool' ? `${p.callId}:${p.status}` : p.kind))).toEqual([
      'a:ok',
      'b:error',
    ]);
    expect(s.streaming).toBe(true);
  });

  it('places confirmation cards after their tool chip', () => {
    const s = run([
      { event: 'tool_start', data: { call_id: 'm', name: 'create_alert', label: 'Alert' } },
      { event: 'tool_result', data: { call_id: 'm', name: 'create_alert', ok: true } },
      { event: 'text_delta', data: { text: 'Please confirm.' } },
      {
        event: 'confirmation_required',
        data: {
          action_id: 'act1',
          call_id: 'm',
          summary: 'Alert when AAPL > $250',
          tool: 'create_alert',
          input: {},
          status: 'pending',
        },
      },
    ]);
    expect(s.messages[1]!.parts.map((p) => p.kind)).toEqual(['tool', 'confirm', 'text']);

    const updated = chatReducer(s, {
      type: 'confirm_update',
      actionId: 'act1',
      status: 'done',
      result: 'Alert #3 created',
    });
    const c = updated.messages[1]!.parts[1];
    expect(c?.kind === 'confirm' && c.confirm.status).toBe('done');
    expect(c?.kind === 'confirm' && c.confirm.result).toBe('Alert #3 created');
  });

  it('records errors and settles pending chips', () => {
    const s = run([
      { event: 'tool_start', data: { call_id: 'a', name: 'get_quote', label: 'A' } },
      { event: 'error', data: { message: 'rate limited', retryable: true } },
    ]);
    const a = s.messages[1]!;
    expect(a.status).toBe('error');
    expect(a.error).toEqual({ message: 'rate limited', retryable: true });
    expect(a.parts[0]?.kind === 'tool' && a.parts[0].status).toBe('error');
    expect(s.streaming).toBe(false);
  });

  it('handles user stop and network failure', () => {
    let s = run([{ event: 'text_delta', data: { text: 'partial' } }]);
    s = chatReducer(s, { type: 'stopped' });
    expect(s.messages[1]!.status).toBe('stopped');
    expect(s.streaming).toBe(false);

    s = chatReducer(run([]), { type: 'failed', message: 'Network error' });
    expect(s.messages[1]!.error?.message).toBe('Network error');
  });
});

describe('fromStored', () => {
  it('folds tool calls and results into one assistant bubble and hides system notes', () => {
    const stored: StoredMessage[] = [
      { id: 1, role: 'user', blocks: [{ type: 'text', text: 'price of apple' }], created_at: '' },
      {
        id: 2,
        role: 'assistant',
        blocks: [
          { type: 'text', text: 'Checking.' },
          { type: 'tool_use', tool_use_id: 'a', tool_name: 'get_quote' },
        ],
        created_at: '',
      },
      {
        id: 3,
        role: 'user',
        blocks: [{ type: 'tool_result', tool_use_id: 'a', tool_name: 'get_quote' }],
        ui: [
          {
            call_id: 'a',
            name: 'get_quote',
            label: 'Fetching AAPL quote…',
            ok: true,
            ui: { type: 'quote_card', data: quote },
          },
        ],
        created_at: '',
      },
      { id: 4, role: 'assistant', blocks: [{ type: 'text', text: 'It is $230.' }], created_at: '' },
      {
        id: 5,
        role: 'user',
        blocks: [{ type: 'text', text: '[system note: user confirmed action act1]' }],
        created_at: '',
      },
      { id: 6, role: 'user', blocks: [{ type: 'text', text: 'thanks' }], created_at: '' },
    ];
    const msgs = fromStored(stored);
    expect(msgs.map((m) => m.role)).toEqual(['user', 'assistant', 'user']);
    const a = msgs[1]!;
    expect(a.parts.map((p) => p.kind)).toEqual(['text', 'tool', 'text']);
    const tool = a.parts[1];
    expect(tool?.kind === 'tool' && tool.label).toBe('Fetching AAPL quote…');
    expect(tool?.kind === 'tool' && tool.ui?.type).toBe('quote_card');
    expect(a.prompt).toBe('price of apple');
  });
});
