// Minimal Server-Sent Events parser for fetch() streams. Native EventSource
// only supports GET, but /api/chat is a POST.

export interface SSEMessage {
  event: string;
  data: string;
}

/**
 * Incremental SSE parser. Feed it text chunks in any split; it calls onMessage
 * for every complete event. Comment lines (": ping") are ignored. Multiple
 * "data:" lines are joined with "\n" per the SSE spec.
 */
export class SSEParser {
  private buffer = '';
  private event = '';
  private data: string[] = [];

  constructor(private readonly onMessage: (msg: SSEMessage) => void) {}

  feed(chunk: string): void {
    this.buffer += chunk;
    // Normalise CRLF / CR line endings. A trailing "\r" may be the first half of
    // a CRLF split across chunks, so leave it for the next feed.
    const pendingCR = this.buffer.endsWith('\r');
    const body = pendingCR ? this.buffer.slice(0, -1) : this.buffer;
    this.buffer = body.replace(/\r\n?/g, '\n') + (pendingCR ? '\r' : '');
    let idx: number;
    while ((idx = this.buffer.indexOf('\n')) >= 0) {
      const line = this.buffer.slice(0, idx);
      this.buffer = this.buffer.slice(idx + 1);
      this.line(line);
    }
  }

  /** Flush a trailing event that wasn't terminated by a blank line. */
  end(): void {
    if (this.buffer) {
      this.line(this.buffer.replace(/\r$/, ''));
      this.buffer = '';
    }
    this.dispatch();
  }

  private line(line: string): void {
    if (line === '') {
      this.dispatch();
      return;
    }
    if (line.startsWith(':')) return; // comment / heartbeat
    const colon = line.indexOf(':');
    const field = colon === -1 ? line : line.slice(0, colon);
    let value = colon === -1 ? '' : line.slice(colon + 1);
    if (value.startsWith(' ')) value = value.slice(1);
    if (field === 'event') this.event = value;
    else if (field === 'data') this.data.push(value);
  }

  private dispatch(): void {
    if (this.data.length > 0) {
      this.onMessage({ event: this.event || 'message', data: this.data.join('\n') });
    }
    this.event = '';
    this.data = [];
  }
}

/** Read an SSE response body to completion, calling onMessage per event. */
export async function readSSE(
  body: ReadableStream<Uint8Array>,
  onMessage: (msg: SSEMessage) => void,
): Promise<void> {
  const parser = new SSEParser(onMessage);
  const decoder = new TextDecoder();
  const reader = body.getReader();
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      // stream: true keeps multi-byte characters split across chunks intact.
      parser.feed(decoder.decode(value, { stream: true }));
    }
    parser.feed(decoder.decode());
    parser.end();
  } finally {
    reader.releaseLock();
  }
}
