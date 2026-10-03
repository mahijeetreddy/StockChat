import { useMemo } from 'react';
import { formatPercent, formatPrice } from '../../lib/format';
import type { CompareData } from '../../lib/types';
import { useIsDark } from '../../state/useTheme';
import { Card, Change } from './Card';
import { seriesColor } from '../../lib/palette';
import { TimeChart } from './TimeChart';

const pct = (v: number) => formatPercent(v);

export function CompareView({ data }: { data: CompareData }) {
  const dark = useIsDark();
  // Colors follow the entity (the model's order), never the rank.
  const colored = useMemo(
    () => data.series.map((s, i) => ({ ...s, color: seriesColor(i, dark) })),
    [data, dark],
  );
  const chartSeries = useMemo(
    () =>
      colored.map((s) => ({
        id: s.symbol,
        color: s.color,
        points: s.points.map((p) => ({ time: p.time, value: p.pct })),
      })),
    [colored],
  );
  const ranked = [...colored].sort((a, b) => b.change_percent - a.change_percent);
  const intraday = data.range === '1D' || data.range === '5D';

  return (
    <Card label={`Comparison over ${data.range}`}>
      <h3 className="text-sm font-semibold tracking-wide text-slate-500 dark:text-slate-400">
        Performance comparison · {data.range}
      </h3>
      <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
        % change from the start of the period
      </p>
      <div className="mt-3">
        <TimeChart
          series={chartSeries}
          kind="line"
          intraday={intraday}
          format={pct}
          baseline={0}
          height={220}
          label={`Normalized % change over ${data.range}: ${ranked
            .map((s) => `${s.symbol} ${formatPercent(s.change_percent)}`)
            .join(', ')}`}
        />
      </div>
      <div className="mt-3 overflow-x-auto">
        <table className="w-full text-sm">
          <caption className="sr-only">Comparison table, sorted by % change</caption>
          <thead>
            <tr className="border-b border-slate-200 text-left text-xs text-slate-500 dark:border-slate-800 dark:text-slate-400">
              <th scope="col" className="py-2 pr-3 font-medium">
                Symbol
              </th>
              <th scope="col" className="py-2 pr-3 text-right font-medium">
                Change
              </th>
              <th scope="col" className="py-2 pr-3 text-right font-medium">
                Start
              </th>
              <th scope="col" className="py-2 pr-3 text-right font-medium">
                End
              </th>
              <th scope="col" className="hidden py-2 pr-3 text-right font-medium sm:table-cell">
                High
              </th>
              <th scope="col" className="hidden py-2 text-right font-medium sm:table-cell">
                Low
              </th>
            </tr>
          </thead>
          <tbody>
            {ranked.map((s) => (
              <tr
                key={s.symbol}
                className="border-b border-slate-100 last:border-0 dark:border-slate-800/60"
              >
                <th scope="row" className="py-2 pr-3 text-left font-semibold">
                  <span className="inline-flex items-center gap-2">
                    <span
                      aria-hidden="true"
                      className="h-0.5 w-4 rounded-full"
                      style={{ backgroundColor: s.color }}
                    />
                    {s.symbol}
                  </span>
                </th>
                <td className="py-2 pr-3 text-right">
                  <Change percent={s.change_percent} />
                </td>
                <td className="py-2 pr-3 text-right tabular-nums">{formatPrice(s.start)}</td>
                <td className="py-2 pr-3 text-right tabular-nums">{formatPrice(s.end)}</td>
                <td className="hidden py-2 pr-3 text-right tabular-nums sm:table-cell">
                  {formatPrice(s.high)}
                </td>
                <td className="hidden py-2 text-right tabular-nums sm:table-cell">
                  {formatPrice(s.low)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
