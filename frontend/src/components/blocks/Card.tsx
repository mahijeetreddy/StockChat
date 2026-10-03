import type { ReactNode } from 'react';
import { arrow, direction, formatPercent, formatSigned } from '../../lib/format';

export function Card({
  children,
  className = '',
  label,
}: {
  children: ReactNode;
  className?: string;
  label?: string;
}) {
  return (
    <section
      aria-label={label}
      className={`rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-800 dark:bg-slate-900 ${className}`}
    >
      {children}
    </section>
  );
}

const tone = {
  up: 'text-emerald-700 dark:text-emerald-400',
  down: 'text-rose-700 dark:text-rose-400',
  flat: 'text-slate-600 dark:text-slate-400',
};

/** Signed change with an arrow, so direction isn't conveyed by color alone. */
export function Change({
  change,
  percent,
  className = '',
}: {
  change?: number;
  percent: number;
  className?: string;
}) {
  const d = direction(percent);
  const label = d === 'up' ? 'up' : d === 'down' ? 'down' : 'unchanged';
  return (
    <span
      className={`inline-flex items-center gap-1 font-medium tabular-nums ${tone[d]} ${className}`}
    >
      <span aria-hidden="true" className="text-[0.7em]">
        {arrow(percent)}
      </span>
      <span className="sr-only">{label} </span>
      {change !== undefined && <span>{formatSigned(change)}</span>}
      <span>{change !== undefined ? `(${formatPercent(percent)})` : formatPercent(percent)}</span>
    </span>
  );
}

export function Badge({
  children,
  tone: t = 'neutral',
}: {
  children: ReactNode;
  tone?: 'neutral' | 'warn' | 'good';
}) {
  const cls = {
    neutral: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300',
    warn: 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300',
    good: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300',
  }[t];
  return <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${cls}`}>{children}</span>;
}
