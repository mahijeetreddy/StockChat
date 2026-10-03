import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../../lib/api';
import { formatRelative } from '../../lib/format';
import type { Alert } from '../../lib/types';
import { IconBell, IconTrash } from '../icons';
import { Badge, Card } from './Card';

export function AlertRow({ alert, compact = false }: { alert: Alert; compact?: boolean }) {
  const queryClient = useQueryClient();
  const [deleted, setDeleted] = useState(false);
  const del = useMutation({
    mutationFn: () => api.deleteAlert(alert.id),
    onSuccess: () => {
      setDeleted(true);
      void queryClient.invalidateQueries({ queryKey: ['alerts'] });
    },
  });
  const status = deleted ? 'deleted' : alert.status;
  return (
    <li
      className={`flex items-start gap-3 ${compact ? 'py-2' : 'px-4 py-3'} ${deleted ? 'opacity-50' : ''}`}
    >
      <IconBell className="mt-0.5 h-4 w-4 shrink-0 text-slate-400" />
      <div className="min-w-0 flex-1">
        <p className={`text-sm ${deleted ? 'line-through' : ''}`}>
          <span className="font-semibold">#{alert.id}</span> {alert.description}
        </p>
        {!compact && (
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            Created {formatRelative(alert.created_at)}
            {alert.last_fired_at && <> · last fired {formatRelative(alert.last_fired_at)}</>}
          </p>
        )}
      </div>
      {status !== 'active' && (
        <Badge tone={status === 'triggered' ? 'good' : 'neutral'}>{status}</Badge>
      )}
      {status !== 'deleted' && (
        <button
          type="button"
          disabled={del.isPending}
          onClick={() => del.mutate()}
          aria-label={`Delete alert #${alert.id}`}
          className="rounded p-1 text-slate-400 hover:bg-slate-100 hover:text-rose-600 disabled:opacity-50 dark:hover:bg-slate-800"
        >
          <IconTrash className="h-3.5 w-3.5" />
        </button>
      )}
    </li>
  );
}

export function AlertList({ alerts }: { alerts: Alert[] }) {
  return (
    <Card label="Your alerts" className="p-0">
      <h3 className="px-4 pt-4 text-sm font-semibold tracking-wide text-slate-500 dark:text-slate-400">
        Your alerts
      </h3>
      {alerts.length === 0 ? (
        <p className="px-4 py-4 text-sm text-slate-500 dark:text-slate-400">
          No alerts yet. Try “Alert me if AAPL goes above 250”.
        </p>
      ) : (
        <ul className="mt-1 divide-y divide-slate-100 dark:divide-slate-800">
          {alerts.map((a) => (
            <AlertRow key={a.id} alert={a} />
          ))}
        </ul>
      )}
    </Card>
  );
}
