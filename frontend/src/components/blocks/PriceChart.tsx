import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../lib/api';
import { formatPrice } from '../../lib/format';
import type { HistoryRange, PriceChartData } from '../../lib/types';
import { useIsDark } from '../../state/useTheme';
import { Card, Change } from './Card';
import { TimeChart } from './TimeChart';

const RANGES: HistoryRange[] = ['1D', '5D', '1M', '3M', '6M', '1Y', '5Y'];

const RANGE_LABEL: Record<HistoryRange, string> = {
  '1D': 'today',
  '5D': 'past 5 days',
  '1M': 'past month',
  '3M': 'past 3 months',
  '6M': 'past 6 months',
  '1Y': 'past year',
  '5Y': 'past 5 years',
};

export function PriceChart({ initial }: { initial: PriceChartData }) {
  const [range, setRange] = useState<HistoryRange>(initial.range);
  const dark = useIsDark();
  const query = useQuery({
    queryKey: ['history', initial.symbol, range],
    queryFn: () => api.history(initial.symbol, range),
    enabled: range !== initial.range,
    staleTime: 60_000,
  });
  const data = range === initial.range ? initial : (query.data ?? null);
  const shown = data ?? initial;
  const color = dark ? '#2dd4bf' : '#0f766e';

  const series = useMemo(
    () => [
      {
        id: shown.symbol,
        color,
        points: shown.candles.map((c) => ({ time: c.time, value: c.close })),
      },
    ],
    [shown, color],
  );

  return (
    <Card label={`${shown.symbol} price chart`}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold tracking-wide text-slate-500 dark:text-slate-400">
            {shown.symbol} · {RANGE_LABEL[shown.range]}
          </h3>
          <div className="mt-1 flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <span className="text-2xl font-semibold tabular-nums">{formatPrice(shown.end)}</span>
            <Change change={shown.change} percent={shown.change_percent} className="text-sm" />
          </div>
        </div>
        <div
          role="tablist"
          aria-label="Chart range"
          className="flex rounded-lg bg-slate-100 p-0.5 text-xs dark:bg-slate-800"
        >
          {RANGES.map((r) => (
            <button
              key={r}
              type="button"
              role="tab"
              aria-selected={r === range}
              onClick={() => setRange(r)}
              className={`rounded-md px-2 py-1 font-medium transition ${
                r === range
                  ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-700 dark:text-white'
                  : 'text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200'
              }`}
            >
              {r}
            </button>
          ))}
        </div>
      </div>

      <div className="relative mt-3">
        <TimeChart
          series={series}
          kind="area"
          intraday={shown.intraday}
          format={formatPrice}
          label={`${shown.symbol} closing prices, ${RANGE_LABEL[shown.range]}: from ${formatPrice(shown.start)} to ${formatPrice(shown.end)}, high ${formatPrice(shown.high)}, low ${formatPrice(shown.low)}`}
        />
        {range !== initial.range && query.isFetching && (
          <div className="absolute inset-0 flex items-center justify-center bg-white/60 text-sm text-slate-500 dark:bg-slate-900/60">
            Loading {range}…
          </div>
        )}
        {range !== initial.range && query.isError && (
          <div className="absolute inset-0 flex items-center justify-center bg-white/80 text-sm text-rose-700 dark:bg-slate-900/80 dark:text-rose-300">
            Couldn’t load {range} data.
          </div>
        )}
      </div>

      <dl className="mt-3 grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
        {[
          ['Start', shown.start],
          ['End', shown.end],
          ['High', shown.high],
          ['Low', shown.low],
        ].map(([k, v]) => (
          <div key={k} className="rounded-lg bg-slate-50 px-3 py-2 dark:bg-slate-800/50">
            <dt className="text-xs text-slate-500 dark:text-slate-400">{k}</dt>
            <dd className="font-medium tabular-nums">{formatPrice(v as number)}</dd>
          </div>
        ))}
      </dl>
    </Card>
  );
}
