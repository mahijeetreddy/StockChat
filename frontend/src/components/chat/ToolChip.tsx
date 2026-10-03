import type { ToolPart } from '../../state/chatReducer';
import { IconCheck, IconSpinner, IconX } from '../icons';

export function ToolChip({ part }: { part: ToolPart }) {
  const styles = {
    pending:
      'border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-700 dark:bg-slate-800/60 dark:text-slate-300',
    ok: 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-300',
    error:
      'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-300',
  }[part.status];
  const statusText = { pending: 'in progress', ok: 'done', error: 'failed' }[part.status];
  return (
    <div
      className={`inline-flex max-w-full items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs ${styles}`}
      title={part.error}
    >
      {part.status === 'pending' && <IconSpinner className="h-3.5 w-3.5 shrink-0" />}
      {part.status === 'ok' && <IconCheck className="h-3.5 w-3.5 shrink-0" />}
      {part.status === 'error' && <IconX className="h-3.5 w-3.5 shrink-0" />}
      <span className="truncate">{part.label.replace(/…$/, '')}</span>
      <span className="sr-only">({statusText})</span>
      {part.status === 'error' && part.error && (
        <span className="truncate opacity-80">: {part.error}</span>
      )}
    </div>
  );
}
