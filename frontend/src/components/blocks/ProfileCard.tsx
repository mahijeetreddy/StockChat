import { useState } from 'react';
import { formatMarketCap } from '../../lib/format';
import type { CompanyProfile } from '../../lib/types';
import { IconExternal } from '../icons';
import { Badge, Card } from './Card';

function Logo({ profile }: { profile: CompanyProfile }) {
  const [failed, setFailed] = useState(false);
  const initials = profile.symbol.slice(0, 2);
  if (profile.logo_url && !failed) {
    return (
      <img
        src={profile.logo_url}
        alt=""
        referrerPolicy="no-referrer"
        onError={() => setFailed(true)}
        className="h-12 w-12 rounded-xl border border-slate-200 bg-white object-contain p-1 dark:border-slate-700"
      />
    );
  }
  return (
    <div
      aria-hidden="true"
      className="flex h-12 w-12 items-center justify-center rounded-xl bg-teal-700/10 font-semibold text-teal-800 dark:bg-teal-400/10 dark:text-teal-300"
    >
      {initials}
    </div>
  );
}

export function ProfileCard({ profile }: { profile: CompanyProfile }) {
  const host = (() => {
    try {
      return profile.web_url ? new URL(profile.web_url).host.replace(/^www\./, '') : '';
    } catch {
      return '';
    }
  })();
  const rows: [string, string][] = [
    ['Industry', profile.industry || '—'],
    ['Exchange', profile.exchange || '—'],
    ['Country', profile.country || '—'],
    ['Market cap', formatMarketCap(profile.market_cap)],
  ];
  return (
    <Card label={`${profile.name} profile`}>
      <div className="flex items-center gap-3">
        <Logo profile={profile} />
        <div className="min-w-0">
          <h3 className="truncate text-lg font-semibold">{profile.name}</h3>
          <div className="mt-0.5 flex flex-wrap items-center gap-2">
            <Badge>{profile.symbol}</Badge>
            {profile.currency && <Badge>{profile.currency}</Badge>}
          </div>
        </div>
      </div>
      <dl className="mt-4 grid grid-cols-2 gap-2 text-sm">
        {rows.map(([k, v]) => (
          <div key={k} className="rounded-lg bg-slate-50 px-3 py-2 dark:bg-slate-800/50">
            <dt className="text-xs text-slate-500 dark:text-slate-400">{k}</dt>
            <dd className="truncate font-medium" title={v}>
              {v}
            </dd>
          </div>
        ))}
      </dl>
      {host && (
        <a
          href={profile.web_url}
          target="_blank"
          rel="noopener noreferrer nofollow"
          className="mt-3 inline-flex items-center gap-1 text-sm text-teal-700 hover:underline dark:text-teal-300"
        >
          {host} <IconExternal className="h-3 w-3" />
        </a>
      )}
    </Card>
  );
}
