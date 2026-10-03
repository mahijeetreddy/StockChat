import {
  arrow,
  direction,
  formatMarketCap,
  formatPercent,
  formatPrice,
  formatRelative,
  formatSigned,
} from './format';

describe('format', () => {
  it('formats prices', () => {
    expect(formatPrice(1234.5)).toBe('$1,234.50');
    expect(formatPrice(0.1234)).toBe('$0.1234');
  });

  it('formats signed values with a real minus sign', () => {
    expect(formatSigned(1.234)).toBe('+1.23');
    expect(formatSigned(-1.234)).toBe('−1.23');
    expect(formatSigned(0)).toBe('0.00');
    expect(formatPercent(-0.5)).toBe('−0.50%');
  });

  it('formats market cap compactly', () => {
    expect(formatMarketCap(3.38e12)).toBe('$3.38T');
    expect(formatMarketCap(0)).toBe('—');
  });

  it('gives a non-color direction cue', () => {
    expect(arrow(1)).toBe('▲');
    expect(arrow(-1)).toBe('▼');
    expect(direction(0)).toBe('flat');
  });

  it('formats relative times', () => {
    const now = new Date('2026-10-03T12:00:00Z');
    expect(formatRelative('2026-10-03T09:00:00Z', now)).toBe('3 hours ago');
    expect(formatRelative('2026-10-02T12:00:00Z', now)).toBe('yesterday');
  });
});
