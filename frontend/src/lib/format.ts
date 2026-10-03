const usd = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const usdSmall = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
  maximumFractionDigits: 4,
});

const compactUsd = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  notation: 'compact',
  maximumFractionDigits: 2,
});

const compact = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 });

/** $1,234.56 (more decimals for sub-dollar prices). */
export function formatPrice(n: number): string {
  return Math.abs(n) < 1 && n !== 0 ? usdSmall.format(n) : usd.format(n);
}

/** $3.38T */
export function formatMarketCap(n: number): string {
  return n > 0 ? compactUsd.format(n) : '—';
}

/** 45.2M */
export function formatVolume(n: number): string {
  return compact.format(n);
}

/** +1.23 / −1.23 using a real minus sign. */
export function formatSigned(n: number, digits = 2): string {
  const s = Math.abs(n).toFixed(digits);
  if (n > 0) return `+${s}`;
  if (n < 0) return `−${s}`;
  return s;
}

/** +1.23% */
export function formatPercent(n: number, digits = 2): string {
  return `${formatSigned(n, digits)}%`;
}

export type Direction = 'up' | 'down' | 'flat';

export function direction(n: number): Direction {
  if (n > 0) return 'up';
  if (n < 0) return 'down';
  return 'flat';
}

/** Arrow glyph so direction is never conveyed by color alone. */
export function arrow(n: number): string {
  return n > 0 ? '▲' : n < 0 ? '▼' : '■';
}

const dateTime = new Intl.DateTimeFormat('en-US', {
  month: 'short',
  day: 'numeric',
  hour: 'numeric',
  minute: '2-digit',
  timeZoneName: 'short',
});

/** "Oct 2, 4:00 PM EDT" in the viewer's time zone. */
export function formatDateTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : dateTime.format(d);
}

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });

/** "3 hours ago" */
export function formatRelative(iso: string, now: Date = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const secs = Math.round((d.getTime() - now.getTime()) / 1000);
  const abs = Math.abs(secs);
  if (abs < 60) return rtf.format(secs, 'second');
  if (abs < 3600) return rtf.format(Math.round(secs / 60), 'minute');
  if (abs < 86400) return rtf.format(Math.round(secs / 3600), 'hour');
  if (abs < 86400 * 30) return rtf.format(Math.round(secs / 86400), 'day');
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
}
