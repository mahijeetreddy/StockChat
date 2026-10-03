import { useCallback, useEffect, useReducer, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { ApiError, api, streamChat } from '../lib/api';
import type { ActionStatus } from '../lib/types';
import { chatReducer, initialChatState } from './chatReducer';

/** Conversation id lives in the URL hash (#/c/<id>) so refresh restores it. */
function idFromHash(): string | null {
  const m = /^#\/c\/([A-Za-z0-9_-]{1,64})$/.exec(window.location.hash);
  return m?.[1] ?? null;
}

function setHash(id: string | null): void {
  const url = id ? `#/c/${id}` : window.location.pathname + window.location.search;
  window.history.replaceState(null, '', url);
}

let seq = 0;

export function useChat() {
  const [state, dispatch] = useReducer(chatReducer, initialChatState);
  const abortRef = useRef<AbortController | null>(null);
  const convoRef = useRef<string | null>(null);
  const queryClient = useQueryClient();
  convoRef.current = state.conversationId;

  const load = useCallback(async (id: string) => {
    abortRef.current?.abort();
    try {
      const detail = await api.getConversation(id);
      dispatch({ type: 'load', conversationId: id, messages: detail.messages });
      setHash(id);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        setHash(null);
        dispatch({ type: 'reset' });
        return;
      }
      throw err;
    }
  }, []);

  // Restore the conversation from the URL on first load and on back/forward.
  useEffect(() => {
    const restore = () => {
      const id = idFromHash();
      if (id && id !== convoRef.current) void load(id).catch(() => undefined);
    };
    restore();
    window.addEventListener('hashchange', restore);
    return () => window.removeEventListener('hashchange', restore);
  }, [load]);

  const send = useCallback(
    async (text: string) => {
      const message = text.trim();
      if (!message || abortRef.current) return;
      const controller = new AbortController();
      abortRef.current = controller;
      dispatch({ type: 'send', text: message, id: `local-${Date.now()}-${seq++}` });

      let finished = false;
      try {
        await streamChat({
          conversationId: convoRef.current,
          message,
          signal: controller.signal,
          onEvent: (event) => {
            if (event.event === 'conversation') {
              convoRef.current = event.data.conversation_id;
              setHash(event.data.conversation_id);
            }
            if (event.event === 'done' || event.event === 'error') finished = true;
            dispatch({ type: 'event', event });
          },
        });
        if (!finished) dispatch({ type: 'failed', message: 'The connection closed before the reply finished.' });
      } catch (err) {
        if (controller.signal.aborted) {
          dispatch({ type: 'stopped' });
        } else {
          const msg =
            err instanceof ApiError
              ? err.message
              : 'Network error: could not reach the server. Is the backend running?';
          dispatch({ type: 'failed', message: msg });
        }
      } finally {
        abortRef.current = null;
        void queryClient.invalidateQueries({ queryKey: ['conversations'] });
        void queryClient.invalidateQueries({ queryKey: ['watchlist'] });
        void queryClient.invalidateQueries({ queryKey: ['alerts'] });
      }
    },
    [queryClient],
  );

  const stop = useCallback(() => abortRef.current?.abort(), []);

  const newChat = useCallback(() => {
    abortRef.current?.abort();
    convoRef.current = null;
    setHash(null);
    dispatch({ type: 'reset' });
  }, []);

  const updateConfirm = useCallback(
    (actionId: string, status: ActionStatus, result?: string) =>
      dispatch({ type: 'confirm_update', actionId, status, result }),
    [],
  );

  return { state, send, stop, newChat, load, updateConfirm };
}
