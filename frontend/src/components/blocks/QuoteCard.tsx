import type { Quote } from '../../lib/types';
import { formatDateTime, formatPrice } from '../../lib/format';
import { Badge, Card, Change } from './Card';

export function QuoteCard({ quote, compact = false }: { quote: Quote; compact?: boolean }) {
  const { low, high, price } = quote;
  const span = high - low;
  const pos = span > 0 ? Math.min(100, Math.max(0, ((price - low) / span) * 100)) : 50;

  return (
    <Card label={`${quote.symbol} quote`} className={compact ? 'p-3' : ''}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-semibold tracking-wide text-slate-500 dark:text-slate-400">
              {quote.symbol}
            </h3>
            {!quote.market_open && <Badge tone="warn">Market closed</Badge>}
          </div>
          <p className={`${compact ? 'text-xl' : 'text-3xl'} mt-1 font-semibold tabular-nums`}>
            {formatPrice(price)}
          </p>
        </div>
        <Change change={quote.change} percent={quote.change_percent} className="mt-1 text-sm" />
      </div>

      {!compact && (
        <>
          <div className="mt-4">
            <div className="flex justify-between text-xs text-slate-500 dark:text-slate-400">
              <span>Day low {formatPrice(low)}</span>
              <span>Day high {formatPrice(high)}</span>
            </div>
            <div
              className="relative mt-1.5 h-1.5 rounded-full bg-slate-200 dark:bg-slate-700"
              role="img"
              aria-label={`Price is ${Math.round(pos)}% of the way from the day's low to high`}
            >
              <div
                className="absolute top-1/2 h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-white bg-teal-700 shadow dark:border-slate-900 dark:bg-teal-400"
                style={{ left: `${pos}%` }}
              />
            </div>
          </div>
          <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-3">
            <div className="flex justify-between gap-2 sm:block">
              <dt className="text-slate-500 dark:text-slate-400">Open</dt>
              <dd className="tabular-nums">{formatPrice(quote.open)}</dd>
            </div>
            <div className="flex justify-between gap-2 sm:block">
              <dt className="text-slate-500 dark:text-slate-400">Prev close</dt>
              <dd className="tabular-nums">{formatPrice(quote.prev_close)}</dd>
            </div>
            <div className="col-span-2 flex justify-between gap-2 sm:col-span-1 sm:block">
              <dt className="text-slate-500 dark:text-slate-400">
                {quote.market_open ? 'As of' : 'Last close'}
              </dt>
              <dd>{formatDateTime(quote.as_of)}</dd>
            </div>
          </dl>
        </>
      )}
    </Card>
  );
}
