import { useEffect, useRef } from 'react';
import type { ChatMessage } from '../../state/chatReducer';
import type { ActionStatus } from '../../lib/types';
import { MessageBubble } from './MessageBubble';

interface Props {
  messages: ChatMessage[];
  streaming: boolean;
  onRetry: (prompt: string) => void;
  onConfirmUpdate: (actionId: string, status: ActionStatus, result?: string) => void;
}

export function MessageList({ messages, streaming, onRetry, onConfirmUpdate }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const stickRef = useRef(true);
  const countRef = useRef(messages.length);

  // Follow new content only while the user is near the bottom. Content can
  // grow after render (lazy charts, images), so watch its size too.
  useEffect(() => {
    const el = containerRef.current;
    const content = contentRef.current;
    if (!el || !content) return;
    const toBottom = () => {
      if (stickRef.current) el.scrollTop = el.scrollHeight;
    };
    const onScroll = () => {
      stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 120;
    };
    el.addEventListener('scroll', onScroll, { passive: true });
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(toBottom);
    ro?.observe(content);
    return () => {
      el.removeEventListener('scroll', onScroll);
      ro?.disconnect();
    };
  }, []);

  useEffect(() => {
    // Sending a new message always jumps to the bottom.
    if (messages.length > countRef.current) stickRef.current = true;
    countRef.current = messages.length;
    const el = containerRef.current;
    if (el && stickRef.current) el.scrollTop = el.scrollHeight;
  }, [messages]);

  const lastIdx = messages.length - 1;
  return (
    <div ref={containerRef} className="flex-1 overflow-y-auto">
      <div
        ref={contentRef}
        role="log"
        aria-live="polite"
        aria-relevant="additions"
        className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-6"
      >
        {messages.map((m, i) => (
          <MessageBubble
            key={m.id}
            message={m}
            onRetry={onRetry}
            onConfirmUpdate={onConfirmUpdate}
            canRetry={i === lastIdx && !streaming}
          />
        ))}
      </div>
    </div>
  );
}
