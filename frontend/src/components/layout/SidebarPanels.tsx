import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../lib/api';
import { arrow, direction, formatPercent, formatPrice } from '../../lib/format';
import { AlertRow } from '../blocks/AlertList';
import { IconStar } from '../icons';

const tone = {
  up: 'text-emerald-700 dark:text-emerald-400',
  down: 'text-rose-700 dark:text-rose-400',
  flat: 'text-slate-500',
};

function SectionTitle({ children, count }: { children: string; count?: number }) {
  return (
    <h2 className="mb-1 flex items-center gap-2 px-2 text-xs font-semibold tracking-wide text-slate-500 uppercase dark:text-slate-400">
      {children}
      {count !== undefined && count > 0 && (
        <span className="rounded-full bg-slate-200 px-1.5 text-[10px] text-slate-600 dark:bg-slate-800 dark:text-slate-300">
          {count}
        </span>
      )}
    </h2>
  );
}

export function WatchlistPanel({ ask }: { ask: (text: string) => void }) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['watchlist'],
    queryFn: api.watchlist,
    refetchInterval: 15_000,
  });
  const quotes = new Map((data?.quotes ?? []).map((q) => [q.symbol, q]));
  const symbols = data?.symbols ?? [];

  return (
    <section className="mb-4" aria-label="Watchlist">
      <SectionTitle count={symbols.length}>Watchlist</SectionTitle>
      {isLoading && <p className="px-2 py-1 text-sm text-slate-400">Loading…</p>}
      {isError && <p className="px-2 py-1 text-sm text-rose-600">Couldn’t load watchlist.</p>}
      {!isLoading && !isError && symbols.length === 0 && (
        <button
          type="button"
          onClick={() => ask('Add AAPL and NVDA to my watchlist')}
          className="flex w-full items-center gap-2 rounded-lg border border-dashed border-slate-300 px-2 py-2 text-left text-sm text-slate-500 hover:border-teal-600 hover:text-teal-800 dark:border-slate-700 dark:text-slate-400 dark:hover:text-teal-300"
        >
          <IconStar className="h-4 w-4 shrink-0" />
          <span>Empty. Ask “Add AAPL to my watchlist”.</span>
        </button>
      )}
      <ul className="flex flex-col">
        {symbols.map((s) => {
          const q = quotes.get(s);
          const d = q ? direction(q.change_percent) : 'flat';
          return (
            <li key={s}>
              <button
                type="button"
                onClick={() => ask(`How is ${s} doing today?`)}
                className="flex w-full items-center justify-between gap-2 rounded-lg px-2 py-1.5 text-sm hover:bg-slate-200/60 dark:hover:bg-slate-800/60"
                title={`Ask about ${s}`}
              >
                <span className="font-semibold">{s}</span>
                {q ? (
                  <span className="flex items-baseline gap-2 tabular-nums">
                    <span>{formatPrice(q.price)}</span>
                    <span className={`w-16 text-right text-xs ${tone[d]}`}>
                      <span aria-hidden="true">{arrow(q.change_percent)} </span>
                      {formatPercent(q.change_percent)}
                    </span>
                  </span>
                ) : (
                  <span className="text-xs text-slate-400">—</span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

export function AlertsPanel() {
  const [expanded, setExpanded] = useState(false);
  const { data: alerts = [] } = useQuery({
    queryKey: ['alerts'],
    queryFn: api.alerts,
    refetchInterval: 30_000,
  });
  const active = alerts.filter((a) => a.status === 'active');
  const shown = expanded ? alerts : active.slice(0, 4);

  return (
    <section className="mb-4" aria-label="Alerts">
      <SectionTitle count={active.length}>Alerts</SectionTitle>
      {alerts.length === 0 ? (
        <p className="px-2 py-1 text-sm text-slate-500 dark:text-slate-400">
          No alerts. Ask “Alert me if TSLA drops below 200”.
        </p>
      ) : (
        <>
          {active.length === 0 && !expanded && (
            <p className="px-2 py-1 text-sm text-slate-500 dark:text-slate-400">
              No active alerts.
            </p>
          )}
          <ul className="px-2">
            {shown.map((a) => (
              <AlertRow key={a.id} alert={a} compact />
            ))}
          </ul>
          {alerts.length > shown.length || expanded ? (
            <button
              type="button"
              onClick={() => setExpanded((e) => !e)}
              className="px-2 text-xs font-medium text-teal-700 hover:underline dark:text-teal-300"
            >
              {expanded ? 'Show active only' : `Show all (${alerts.length})`}
            </button>
          ) : null}
        </>
      )}
    </section>
  );
}

export function SidebarPanels({ ask }: { ask: (text: string) => void }) {
  return (
    <>
      <WatchlistPanel ask={ask} />
      <AlertsPanel />
    </>
  );
}
