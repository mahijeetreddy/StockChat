import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { ApiError, api } from '../../lib/api';
import type { ActionStatus, ConfirmData } from '../../lib/types';
import { IconCheck, IconSpinner, IconX } from '../icons';

interface Props {
  confirm: ConfirmData;
  onUpdate: (actionId: string, status: ActionStatus, result?: string) => void;
}

const statusText: Record<ActionStatus, string> = {
  pending: 'Waiting for your confirmation',
  executing: 'Applying…',
  done: 'Confirmed',
  failed: 'Failed',
  cancelled: 'Cancelled',
  expired: 'Expired (not applied)',
};

function isStatus(v: unknown): v is ActionStatus {
  return typeof v === 'string' && v in statusText;
}

export function ConfirmCard({ confirm, onUpdate }: Props) {
  const [busy, setBusy] = useState<'confirm' | 'cancel' | null>(null);
  const [error, setError] = useState<string | null>(null);
  const queryClient = useQueryClient();
  const pending = confirm.status === 'pending';

  const act = async (kind: 'confirm' | 'cancel') => {
    setBusy(kind);
    setError(null);
    try {
      const res =
        kind === 'confirm'
          ? await api.confirmAction(confirm.action_id)
          : await api.cancelAction(confirm.action_id);
      onUpdate(confirm.action_id, res.status, res.result);
      void queryClient.invalidateQueries({ queryKey: ['watchlist'] });
      void queryClient.invalidateQueries({ queryKey: ['alerts'] });
    } catch (err) {
      if (
        err instanceof ApiError &&
        err.status === 409 &&
        err.body &&
        typeof err.body === 'object'
      ) {
        const b = err.body as { status?: unknown; result?: unknown };
        if (isStatus(b.status)) {
          onUpdate(
            confirm.action_id,
            b.status,
            typeof b.result === 'string' ? b.result : undefined,
          );
          return;
        }
      }
      setError(err instanceof Error ? err.message : 'Something went wrong');
    } finally {
      setBusy(null);
    }
  };

  const tone =
    confirm.status === 'done'
      ? 'border-emerald-300 dark:border-emerald-800'
      : pending
        ? 'border-amber-300 dark:border-amber-700'
        : 'border-slate-200 dark:border-slate-800';

  return (
    <section
      aria-label="Action needs confirmation"
      className={`rounded-xl border-2 bg-white p-4 shadow-sm dark:bg-slate-900 ${tone}`}
    >
      <p className="text-xs font-semibold tracking-wide text-slate-500 uppercase dark:text-slate-400">
        {statusText[confirm.status]}
      </p>
      <p className="mt-1 font-medium">{confirm.summary}</p>
      {confirm.result && confirm.status !== 'pending' && (
        <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">{confirm.result}</p>
      )}
      {error && (
        <p role="alert" className="mt-2 text-sm text-rose-700 dark:text-rose-400">
          {error}
        </p>
      )}
      {pending && (
        <div className="mt-3 flex gap-2">
          <button
            type="button"
            disabled={busy !== null}
            onClick={() => void act('confirm')}
            className="inline-flex items-center gap-1.5 rounded-lg bg-teal-700 px-3 py-1.5 text-sm font-medium text-white hover:bg-teal-800 disabled:opacity-50 dark:bg-teal-600 dark:hover:bg-teal-500"
          >
            {busy === 'confirm' ? <IconSpinner /> : <IconCheck />} Confirm
          </button>
          <button
            type="button"
            disabled={busy !== null}
            onClick={() => void act('cancel')}
            className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 px-3 py-1.5 text-sm font-medium hover:bg-slate-100 disabled:opacity-50 dark:border-slate-700 dark:hover:bg-slate-800"
          >
            {busy === 'cancel' ? <IconSpinner /> : <IconX />} Cancel
          </button>
        </div>
      )}
    </section>
  );
}
