// Validated categorical palette (dataviz reference palette, slots 1-5),
// stepped separately for the light and dark chart surfaces.
export const SERIES_COLORS = {
  light: ['#2a78d6', '#eb6834', '#1baf7a', '#eda100', '#e87ba4'],
  dark: ['#3987e5', '#d95926', '#199e70', '#c98500', '#d55181'],
};

export function seriesColor(i: number, dark: boolean): string {
  const p = dark ? SERIES_COLORS.dark : SERIES_COLORS.light;
  return p[i % p.length] ?? '#888888';
}
