import { useEffect, useState } from 'react';
import { AUTH_REQUIRED_EVENT, setToken } from '../../lib/api';

/** Asks for the shared APP_TOKEN when the API answers 401. */
export function TokenGate() {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState('');

  useEffect(() => {
    const onAuth = () => setOpen(true);
    window.addEventListener(AUTH_REQUIRED_EVENT, onAuth);
    return () => window.removeEventListener(AUTH_REQUIRED_EVENT, onAuth);
  }, []);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/50 p-4">
      <form
        role="dialog"
        aria-modal="true"
        aria-labelledby="token-title"
        onSubmit={(e) => {
          e.preventDefault();
          setToken(value.trim());
          window.location.reload();
        }}
        className="w-full max-w-sm rounded-2xl bg-white p-5 shadow-xl dark:bg-slate-900"
      >
        <h2 id="token-title" className="text-lg font-semibold">
          Access token required
        </h2>
        <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">
          This StockChat server is protected. Enter the token it was started with (APP_TOKEN).
        </p>
        <label htmlFor="token" className="sr-only">
          Access token
        </label>
        <input
          id="token"
          type="password"
          autoFocus
          value={value}
          onChange={(e) => setValue(e.target.value)}
          className="mt-4 w-full rounded-lg border border-slate-300 bg-transparent px-3 py-2 outline-none focus:border-teal-600 focus:ring-2 focus:ring-teal-600/20 dark:border-slate-700"
        />
        <button
          type="submit"
          disabled={!value.trim()}
          className="mt-4 w-full rounded-lg bg-teal-700 px-3 py-2 text-sm font-medium text-white hover:bg-teal-800 disabled:opacity-50 dark:bg-teal-600 dark:hover:bg-teal-500"
        >
          Continue
        </button>
      </form>
    </div>
  );
}
