import type { ReactNode } from 'react';
import { IconMenu, IconMoon, IconSun } from '../icons';
import { MarketBadge } from './MarketBadge';

interface Props {
  theme: 'light' | 'dark';
  onToggleTheme: () => void;
  onToggleSidebar: () => void;
  actions?: ReactNode;
}

export function Header({ theme, onToggleTheme, onToggleSidebar, actions }: Props) {
  return (
    <header className="flex h-14 shrink-0 items-center gap-3 border-b border-slate-200 bg-white/80 px-4 backdrop-blur dark:border-slate-800 dark:bg-slate-950/80">
      <button
        type="button"
        onClick={onToggleSidebar}
        aria-label="Toggle sidebar"
        className="rounded-lg p-2 text-slate-600 hover:bg-slate-100 md:hidden dark:text-slate-300 dark:hover:bg-slate-800"
      >
        <IconMenu className="h-5 w-5" />
      </button>
      <div className="flex items-center gap-2">
        <img src="/favicon.svg" alt="" className="h-7 w-7" />
        <h1 className="text-lg font-semibold tracking-tight">StockChat</h1>
      </div>
      <MarketBadge />
      <div className="ml-auto flex items-center gap-1">
        {actions}
        <button
          type="button"
          onClick={onToggleTheme}
          aria-label={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          className="rounded-lg p-2 text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
        >
          {theme === 'dark' ? <IconSun className="h-5 w-5" /> : <IconMoon className="h-5 w-5" />}
        </button>
      </div>
    </header>
  );
}
