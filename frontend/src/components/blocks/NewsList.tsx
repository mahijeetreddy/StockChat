import { formatRelative } from '../../lib/format';
import type { NewsListData } from '../../lib/types';
import { IconExternal } from '../icons';
import { Card } from './Card';

export function NewsList({ data }: { data: NewsListData }) {
  return (
    <Card label={`${data.symbol} news`} className="p-0">
      <h3 className="px-4 pt-4 text-sm font-semibold tracking-wide text-slate-500 dark:text-slate-400">
        {data.symbol} · latest news
      </h3>
      {data.items.length === 0 ? (
        <p className="px-4 py-4 text-sm text-slate-500 dark:text-slate-400">
          No news in the last 7 days.
        </p>
      ) : (
        <ul className="mt-2 divide-y divide-slate-100 dark:divide-slate-800">
          {data.items.map((n, i) => (
            <li key={`${n.url}-${i}`} className="px-4 py-3">
              {/* Third-party text is rendered as plain text, never HTML. */}
              {n.url ? (
                <a
                  href={n.url}
                  target="_blank"
                  rel="noopener noreferrer nofollow"
                  className="group inline-flex items-start gap-1 font-medium hover:text-teal-700 dark:hover:text-teal-300"
                >
                  <span>{n.headline}</span>
                  <IconExternal className="mt-1 h-3 w-3 shrink-0 opacity-50 group-hover:opacity-100" />
                  <span className="sr-only">(opens in a new tab)</span>
                </a>
              ) : (
                <span className="font-medium">{n.headline}</span>
              )}
              <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
                {n.source} · <time dateTime={n.published_at}>{formatRelative(n.published_at)}</time>
              </p>
              {n.summary && (
                <p className="mt-1 line-clamp-2 text-sm text-slate-600 dark:text-slate-300">
                  {n.summary}
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
