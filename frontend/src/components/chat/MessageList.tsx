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
  const endRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const stickRef = useRef(true);

  // Follow new content only while the user is near the bottom.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const onScroll = () => {
      stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 120;
    };
    el.addEventListener('scroll', onScroll, { passive: true });
    return () => el.removeEventListener('scroll', onScroll);
  }, []);

  useEffect(() => {
    if (stickRef.current) endRef.current?.scrollIntoView({ block: 'end' });
  }, [messages]);

  const lastIdx = messages.length - 1;
  return (
    <div ref={containerRef} className="flex-1 overflow-y-auto">
      <div
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
        <div ref={endRef} />
      </div>
    </div>
  );
}
