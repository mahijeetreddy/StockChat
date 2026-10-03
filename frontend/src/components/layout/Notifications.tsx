import { useEffect, useRef, useState } from 'react';
import { formatPrice, formatRelative } from '../../lib/format';
import type { Toast } from '../../state/useNotifications';
import { IconBell, IconX } from '../icons';

export function Toasts({
  toasts,
  onDismiss,
}: {
  toasts: Toast[];
  onDismiss: (key: string) => void;
}) {
  return (
    <div
      aria-live="assertive"
      className="pointer-events-none fixed right-4 bottom-4 z-50 flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2"
    >
      {toasts.map((t) => (
        <div
          key={t.key}
          role="status"
          className="pointer-events-auto flex items-start gap-3 rounded-xl border border-amber-300 bg-white p-3 shadow-lg dark:border-amber-700 dark:bg-slate-900"
        >
          <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300">
            <IconBell />
          </div>
          <div className="min-w-0 flex-1">
            <p className="text-sm font-semibold">
              {t.symbol} alert · {formatPrice(t.price)}
            </p>
            <p className="text-sm text-slate-600 dark:text-slate-300">{t.message}</p>
          </div>
          <button
            type="button"
            onClick={() => onDismiss(t.key)}
            aria-label="Dismiss notification"
            className="rounded p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-700 dark:hover:bg-slate-800"
          >
            <IconX className="h-3.5 w-3.5" />
          </button>
        </div>
      ))}
    </div>
  );
}

export function NotificationBell({
  items,
  unread,
  onOpen,
}: {
  items: Toast[];
  unread: number;
  onOpen: () => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('mousedown', onDoc);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDoc);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-label={unread > 0 ? `Notifications (${unread} new)` : 'Notifications'}
        aria-expanded={open}
        onClick={() => {
          setOpen((o) => !o);
          onOpen();
        }}
        className="relative rounded-lg p-2 text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
      >
        <IconBell className="h-5 w-5" />
        {unread > 0 && (
          <span className="absolute top-1 right-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-rose-600 px-1 text-[10px] font-bold text-white">
            {unread}
          </span>
        )}
      </button>
      {open && (
        <div className="absolute right-0 z-40 mt-2 w-80 max-w-[calc(100vw-2rem)] rounded-xl border border-slate-200 bg-white shadow-xl dark:border-slate-800 dark:bg-slate-900">
          <p className="border-b border-slate-100 px-4 py-2 text-xs font-semibold tracking-wide text-slate-500 uppercase dark:border-slate-800 dark:text-slate-400">
            Alert notifications
          </p>
          {items.length === 0 ? (
            <p className="px-4 py-4 text-sm text-slate-500 dark:text-slate-400">
              Nothing yet. Fired alerts show up here while the app is open.
            </p>
          ) : (
            <ul className="max-h-80 divide-y divide-slate-100 overflow-y-auto dark:divide-slate-800">
              {items.map((n) => (
                <li key={n.key} className="px-4 py-2.5">
                  <p className="text-sm">{n.message}</p>
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    {formatRelative(n.fired_at)}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
