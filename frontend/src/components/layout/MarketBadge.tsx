import { useQuery } from '@tanstack/react-query';
import { api } from '../../lib/api';
import { formatDateTime } from '../../lib/format';

export function MarketBadge() {
  const { data, isError } = useQuery({
    queryKey: ['market-status'],
    queryFn: api.marketStatus,
    refetchInterval: 60_000,
  });
  if (isError || !data) return null;
  const title = data.open
    ? 'US market is open (regular session)'
    : data.next_open
      ? `US market is closed. Next open: ${formatDateTime(data.next_open)}`
      : 'US market is closed';
  return (
    <span
      title={title}
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium whitespace-nowrap ${
        data.open
          ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-300'
          : 'bg-slate-200 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
      }`}
    >
      <span
        aria-hidden="true"
        className={`h-1.5 w-1.5 rounded-full ${data.open ? 'animate-pulse bg-emerald-600' : 'bg-slate-500'}`}
      />
      <span className="hidden sm:inline">Market </span>
      {data.open ? 'open' : 'closed'}
      <span className="sr-only">. {title}</span>
    </span>
  );
}
