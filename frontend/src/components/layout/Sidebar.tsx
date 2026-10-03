import { useEffect, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../lib/api';
import { formatRelative } from '../../lib/format';
import { IconPlus, IconTrash } from '../icons';

interface Props {
  open: boolean;
  onClose: () => void;
  activeId: string | null;
  onSelect: (id: string) => void;
  onNewChat: () => void;
  children?: ReactNode; // extra panels (watchlist, alerts)
}

export function Sidebar({ open, onClose, activeId, onSelect, onNewChat, children }: Props) {
  const queryClient = useQueryClient();
  const { data: conversations = [] } = useQuery({
    queryKey: ['conversations'],
    queryFn: api.listConversations,
  });
  // Close the mobile drawer with Escape.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  const del = useMutation({
    mutationFn: api.deleteConversation,
    onSuccess: (_, id) => {
      void queryClient.invalidateQueries({ queryKey: ['conversations'] });
      if (id === activeId) onNewChat();
    },
  });

  return (
    <>
      {open && (
        <div
          className="fixed inset-0 z-20 bg-slate-950/40 md:hidden"
          onClick={onClose}
          aria-hidden="true"
        />
      )}
      <aside
        className={`fixed inset-y-0 left-0 z-30 flex w-72 flex-col border-r border-slate-200 bg-slate-50 transition-transform md:static md:translate-x-0 dark:border-slate-800 dark:bg-slate-950 ${
          open ? 'translate-x-0' : '-translate-x-full'
        }`}
        aria-label="Sidebar"
      >
        <div className="p-3">
          <button
            type="button"
            onClick={() => {
              onNewChat();
              onClose();
            }}
            className="flex w-full items-center justify-center gap-2 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium hover:border-teal-600 hover:text-teal-800 dark:border-slate-700 dark:bg-slate-900 dark:hover:border-teal-500 dark:hover:text-teal-300"
          >
            <IconPlus /> New chat
          </button>
        </div>
        <div className="flex-1 overflow-y-auto px-3 pb-3">
          {children}
          <h2 className="mt-2 mb-1 px-2 text-xs font-semibold tracking-wide text-slate-500 uppercase dark:text-slate-400">
            Chats
          </h2>
          {conversations.length === 0 && (
            <p className="px-2 py-1 text-sm text-slate-500 dark:text-slate-400">No chats yet.</p>
          )}
          <ul className="flex flex-col gap-0.5">
            {conversations.map((c) => (
              <li key={c.id} className="group relative">
                <button
                  type="button"
                  onClick={() => {
                    onSelect(c.id);
                    onClose();
                  }}
                  aria-current={c.id === activeId ? 'page' : undefined}
                  className={`w-full rounded-lg px-2 py-1.5 pr-8 text-left text-sm ${
                    c.id === activeId
                      ? 'bg-teal-700/10 text-teal-900 dark:bg-teal-400/10 dark:text-teal-200'
                      : 'text-slate-700 hover:bg-slate-200/60 dark:text-slate-300 dark:hover:bg-slate-800/60'
                  }`}
                >
                  <span className="block truncate">{c.title}</span>
                  <span className="block text-xs text-slate-500 dark:text-slate-500">
                    {formatRelative(c.updated_at)}
                  </span>
                </button>
                <button
                  type="button"
                  aria-label={`Delete chat "${c.title}"`}
                  onClick={() => del.mutate(c.id)}
                  className="absolute top-1/2 right-1 -translate-y-1/2 rounded p-1 text-slate-400 opacity-0 group-hover:opacity-100 hover:bg-slate-200 hover:text-rose-600 focus:opacity-100 dark:hover:bg-slate-800"
                >
                  <IconTrash className="h-3.5 w-3.5" />
                </button>
              </li>
            ))}
          </ul>
        </div>
      </aside>
    </>
  );
}
