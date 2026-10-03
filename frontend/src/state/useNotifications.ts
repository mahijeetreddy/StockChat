import { useCallback, useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { getToken } from '../lib/api';
import type { Notification } from '../lib/types';

export interface Toast extends Notification {
  key: string;
}

let seq = 0;

/**
 * Subscribes to /api/stream/notifications (SSE). EventSource reconnects on its
 * own; it can't send headers, so the optional token goes in the query string.
 */
export function useNotifications() {
  const [items, setItems] = useState<Toast[]>([]);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [unread, setUnread] = useState(0);
  const queryClient = useQueryClient();

  const dismiss = useCallback((key: string) => {
    setToasts((t) => t.filter((x) => x.key !== key));
  }, []);

  useEffect(() => {
    if (typeof EventSource === 'undefined') return;
    const token = getToken();
    const url = `/api/stream/notifications${token ? `?token=${encodeURIComponent(token)}` : ''}`;
    const es = new EventSource(url);
    const onAlert = (ev: MessageEvent<string>) => {
      let n: Notification;
      try {
        n = JSON.parse(ev.data) as Notification;
      } catch {
        return;
      }
      const toast = { ...n, key: `n${seq++}` };
      setItems((x) => [toast, ...x].slice(0, 20));
      setToasts((x) => [...x, toast].slice(-3));
      setUnread((u) => u + 1);
      void queryClient.invalidateQueries({ queryKey: ['alerts'] });
      window.setTimeout(() => dismiss(toast.key), 8000);
    };
    es.addEventListener('alert', onAlert);
    return () => {
      es.removeEventListener('alert', onAlert);
      es.close();
    };
  }, [queryClient, dismiss]);

  const markRead = useCallback(() => setUnread(0), []);
  return { items, toasts, unread, dismiss, markRead };
}
