import { lazy, Suspense } from 'react';
import type { UIBlock } from '../../lib/types';
import { AlertList } from './AlertList';
import { NewsList } from './NewsList';
import { ProfileCard } from './ProfileCard';
import { QuoteCard } from './QuoteCard';

// Charts pull in lightweight-charts; load them on first use.
const PriceChart = lazy(() => import('./PriceChart').then((m) => ({ default: m.PriceChart })));
const CompareView = lazy(() => import('./CompareView').then((m) => ({ default: m.CompareView })));

function ChartSkeleton() {
  return (
    <div
      aria-hidden="true"
      className="h-[340px] animate-pulse rounded-xl border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900"
    />
  );
}

/** Renders a structured tool result. Data always comes from Go structs. */
export function BlockView({ block }: { block: UIBlock }) {
  switch (block.type) {
    case 'quote_card':
      return <QuoteCard quote={block.data} />;
    case 'quote_list':
      return block.data.quotes.length === 0 ? (
        <p className="text-sm text-slate-500 dark:text-slate-400">Your watchlist is empty.</p>
      ) : (
        <div className="grid gap-2 sm:grid-cols-2">
          {block.data.quotes.map((q) => (
            <QuoteCard key={q.symbol} quote={q} compact />
          ))}
        </div>
      );
    case 'price_chart':
      return (
        <Suspense fallback={<ChartSkeleton />}>
          <PriceChart initial={block.data} />
        </Suspense>
      );
    case 'compare':
      return (
        <Suspense fallback={<ChartSkeleton />}>
          <CompareView data={block.data} />
        </Suspense>
      );
    case 'news_list':
      return <NewsList data={block.data} />;
    case 'profile_card':
      return <ProfileCard profile={block.data} />;
    case 'alert_list':
      return <AlertList alerts={block.data.alerts} />;
    default:
      return null;
  }
}
