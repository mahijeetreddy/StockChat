import { IconChart } from '../icons';

const SUGGESTIONS = [
  "What's Apple trading at?",
  'How has Tesla done this month?',
  'Compare NVDA, AMD, and INTC over 6 months',
  'Any news on Microsoft?',
  'Tell me about Costco',
  'Alert me if AAPL goes above 250',
];

export function EmptyState({ onPick }: { onPick: (text: string) => void }) {
  return (
    <div className="mx-auto flex max-w-2xl flex-1 flex-col items-center justify-center px-4 py-10 text-center">
      <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-teal-700/10 text-teal-700 dark:bg-teal-400/10 dark:text-teal-300">
        <IconChart className="h-6 w-6" />
      </div>
      <h2 className="text-2xl font-semibold tracking-tight">Ask about any US stock</h2>
      <p className="mt-2 max-w-md text-slate-600 dark:text-slate-400">
        Prices, charts, comparisons, news, and alerts, all from live market data. Numbers come
        straight from the data provider, not from the AI.
      </p>
      <div className="mt-8 flex flex-wrap justify-center gap-2">
        {SUGGESTIONS.map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => onPick(s)}
            className="rounded-full border border-slate-300 bg-white px-3.5 py-1.5 text-sm text-slate-700 shadow-sm transition hover:border-teal-600 hover:text-teal-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:border-teal-500 dark:hover:text-teal-300"
          >
            {s}
          </button>
        ))}
      </div>
    </div>
  );
}
