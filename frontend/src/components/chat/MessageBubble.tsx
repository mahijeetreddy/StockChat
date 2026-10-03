import type { ChatMessage } from '../../state/chatReducer';
import type { ActionStatus } from '../../lib/types';
import { BlockView } from '../blocks/BlockView';
import { ConfirmCard } from '../blocks/ConfirmCard';
import { IconRetry } from '../icons';
import { Markdown } from './Markdown';
import { ToolChip } from './ToolChip';

interface Props {
  message: ChatMessage;
  onRetry?: (prompt: string) => void;
  onConfirmUpdate: (actionId: string, status: ActionStatus, result?: string) => void;
  canRetry: boolean;
}

export function MessageBubble({ message, onRetry, onConfirmUpdate, canRetry }: Props) {
  if (message.role === 'user') {
    const text = message.parts[0]?.kind === 'text' ? message.parts[0].text : '';
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-2xl rounded-br-sm bg-teal-700 px-4 py-2.5 whitespace-pre-wrap text-white shadow-sm dark:bg-teal-600">
          <span className="sr-only">You said: </span>
          {text}
        </div>
      </div>
    );
  }

  const thinking = message.status === 'streaming' && message.parts.length === 0;
  return (
    <div className="flex flex-col gap-3" aria-busy={message.status === 'streaming'}>
      <span className="sr-only">Assistant:</span>
      {thinking && (
        <div className="flex items-center gap-1.5 py-2" aria-label="Assistant is thinking">
          {[0, 150, 300].map((d) => (
            <span
              key={d}
              className="h-2 w-2 animate-bounce rounded-full bg-slate-400 dark:bg-slate-500"
              style={{ animationDelay: `${d}ms` }}
            />
          ))}
        </div>
      )}
      {message.parts.map((part, i) => {
        switch (part.kind) {
          case 'text':
            return <Markdown key={i} text={part.text} />;
          case 'tool':
            return (
              <div key={part.callId} className="flex flex-col gap-2">
                <div>
                  <ToolChip part={part} />
                </div>
                {part.ui && <BlockView block={part.ui} />}
              </div>
            );
          case 'confirm':
            return (
              <ConfirmCard
                key={part.confirm.action_id}
                confirm={part.confirm}
                onUpdate={onConfirmUpdate}
              />
            );
        }
      })}
      {message.status === 'stopped' && (
        <p className="text-xs text-slate-500 italic dark:text-slate-400">Stopped.</p>
      )}
      {message.status === 'error' && message.error && (
        <div
          role="alert"
          className="flex flex-wrap items-center gap-3 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-800 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200"
        >
          <span className="flex-1">{message.error.message}</span>
          {canRetry && message.error.retryable && message.prompt && onRetry && (
            <button
              type="button"
              onClick={() => onRetry(message.prompt ?? '')}
              className="inline-flex items-center gap-1 rounded-md border border-rose-300 px-2 py-1 text-xs font-medium hover:bg-rose-100 dark:border-rose-800 dark:hover:bg-rose-900/50"
            >
              <IconRetry className="h-3.5 w-3.5" /> Retry
            </button>
          )}
        </div>
      )}
    </div>
  );
}
