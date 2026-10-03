import { useEffect, useRef } from 'react';
import {
  AreaSeries,
  ColorType,
  CrosshairMode,
  LineSeries,
  LineStyle,
  createChart,
  type IChartApi,
  type Time,
  type UTCTimestamp,
} from 'lightweight-charts';
import { useIsDark } from '../../state/useTheme';

export interface ChartSeries {
  id: string;
  color: string;
  points: { time: string; value: number }[];
}

interface Props {
  series: ChartSeries[];
  kind: 'area' | 'line';
  intraday: boolean;
  format: (v: number) => string;
  height?: number;
  /** Draw a dashed baseline (e.g. 0% on comparison charts). */
  baseline?: number;
  label: string;
}

const NY = 'America/New_York';

function toTs(iso: string): UTCTimestamp {
  return Math.floor(new Date(iso).getTime() / 1000) as UTCTimestamp;
}

function tsDate(t: Time): Date {
  return new Date((t as number) * 1000);
}

export function TimeChart({
  series,
  kind,
  intraday,
  format,
  height = 240,
  baseline,
  label,
}: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const dark = useIsDark();

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ink = dark ? '#c3c2b7' : '#52514e';
    const grid = dark ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)';
    const dateFmt = new Intl.DateTimeFormat('en-US', {
      timeZone: NY,
      month: 'short',
      day: 'numeric',
      ...(intraday ? { hour: 'numeric', minute: '2-digit' } : { year: 'numeric' }),
    });
    const tickFmt = new Intl.DateTimeFormat('en-US', {
      timeZone: NY,
      ...(intraday ? { hour: 'numeric', minute: '2-digit' } : { month: 'short', day: 'numeric' }),
    });

    const chart: IChartApi = createChart(el, {
      autoSize: true,
      height,
      layout: {
        background: { type: ColorType.Solid, color: 'transparent' },
        textColor: ink,
        fontFamily: 'inherit',
        fontSize: 11,
        attributionLogo: true,
      },
      grid: { vertLines: { visible: false }, horzLines: { color: grid } },
      rightPriceScale: { borderVisible: false, scaleMargins: { top: 0.12, bottom: 0.08 } },
      timeScale: {
        borderVisible: false,
        timeVisible: intraday,
        secondsVisible: false,
        fixLeftEdge: true,
        fixRightEdge: true,
        tickMarkFormatter: (t: Time) => tickFmt.format(tsDate(t)),
      },
      localization: {
        priceFormatter: format,
        timeFormatter: (t: Time) => dateFmt.format(tsDate(t)),
      },
      crosshair: {
        mode: CrosshairMode.Magnet,
        vertLine: {
          color: ink,
          style: LineStyle.Dashed,
          width: 1,
          labelBackgroundColor: dark ? '#334155' : '#475569',
        },
        horzLine: {
          color: ink,
          style: LineStyle.Dashed,
          width: 1,
          labelBackgroundColor: dark ? '#334155' : '#475569',
        },
      },
      handleScroll: false,
      handleScale: false,
    });

    for (const s of series) {
      const data = s.points.map((p) => ({ time: toTs(p.time), value: p.value }));
      if (kind === 'area') {
        const area = chart.addSeries(AreaSeries, {
          lineColor: s.color,
          lineWidth: 2,
          topColor: `${s.color}55`,
          bottomColor: `${s.color}05`,
          priceLineVisible: false,
          lastValueVisible: true,
          crosshairMarkerRadius: 4,
        });
        area.setData(data);
      } else {
        const line = chart.addSeries(LineSeries, {
          color: s.color,
          lineWidth: 2,
          priceLineVisible: false,
          lastValueVisible: false,
          crosshairMarkerRadius: 4,
        });
        line.setData(data);
        if (baseline !== undefined && s === series[0]) {
          line.createPriceLine({
            price: baseline,
            color: ink,
            lineWidth: 1,
            lineStyle: LineStyle.Dotted,
            axisLabelVisible: false,
            title: '',
          });
        }
      }
    }
    chart.timeScale().fitContent();
    return () => chart.remove();
  }, [series, kind, intraday, format, height, baseline, dark]);

  return <div ref={ref} role="img" aria-label={label} style={{ height }} className="w-full" />;
}
