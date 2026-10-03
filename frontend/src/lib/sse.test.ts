import { SSEParser, readSSE, type SSEMessage } from './sse';

function parse(chunks: string[]): SSEMessage[] {
  const out: SSEMessage[] = [];
  const p = new SSEParser((m) => out.push(m));
  chunks.forEach((c) => p.feed(c));
  p.end();
  return out;
}

describe('SSEParser', () => {
  it('parses complete events', () => {
    expect(parse(['event: text_delta\ndata: {"text":"hi"}\n\n'])).toEqual([
      { event: 'text_delta', data: '{"text":"hi"}' },
    ]);
  });

  it('handles chunk boundaries anywhere, including mid-event and mid-line', () => {
    const stream = 'event: a\ndata: {"x":1}\n\nevent: b\ndata: {"y":2}\n\n';
    const expected = [
      { event: 'a', data: '{"x":1}' },
      { event: 'b', data: '{"y":2}' },
    ];
    for (let i = 1; i < stream.length; i++) {
      expect(parse([stream.slice(0, i), stream.slice(i)])).toEqual(expected);
    }
    // One character at a time.
    expect(parse(stream.split(''))).toEqual(expected);
  });

  it('joins multi-line data with newlines', () => {
    expect(parse(['event: x\ndata: line1\ndata: line2\n\n'])).toEqual([
      { event: 'x', data: 'line1\nline2' },
    ]);
  });

  it('ignores heartbeat comments', () => {
    expect(parse([': ping\n\n', 'event: done\ndata: {}\n\n', ': ping\n\n'])).toEqual([
      { event: 'done', data: '{}' },
    ]);
  });

  it('accepts CRLF line endings, split across chunks', () => {
    expect(parse(['event: a\r', '\ndata: 1\r\n\r\n'])).toEqual([{ event: 'a', data: '1' }]);
  });

  it('defaults the event name to "message" and handles missing space', () => {
    expect(parse(['data:raw\n\n'])).toEqual([{ event: 'message', data: 'raw' }]);
  });

  it('flushes an unterminated final event on end()', () => {
    expect(parse(['event: done\ndata: {}'])).toEqual([{ event: 'done', data: '{}' }]);
  });
});

describe('readSSE', () => {
  it('reads a byte stream, including multi-byte characters split across chunks', async () => {
    const bytes = new TextEncoder().encode('event: t\ndata: {"label":"Fetching…"}\n\n');
    const split = 30; // inside the 3-byte ellipsis
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(bytes.slice(0, split));
        c.enqueue(bytes.slice(split));
        c.close();
      },
    });
    const out: SSEMessage[] = [];
    await readSSE(body, (m) => out.push(m));
    expect(out).toEqual([{ event: 't', data: '{"label":"Fetching…"}' }]);
  });
});
