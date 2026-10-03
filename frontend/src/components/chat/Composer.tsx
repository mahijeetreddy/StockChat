import { forwardRef, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react';
import { IconSend, IconStop } from '../icons';

const MAX_CHARS = 2000;

interface Props {
  streaming: boolean;
  onSend: (text: string) => void;
  onStop: () => void;
}

export const Composer = forwardRef<HTMLTextAreaElement, Props>(function Composer(
  { streaming, onSend, onStop },
  ref,
) {
  const [text, setText] = useState('');
  const innerRef = useRef<HTMLTextAreaElement | null>(null);
  const trimmed = text.trim();
  const tooLong = text.length > MAX_CHARS;

  const submit = () => {
    if (!trimmed || streaming || tooLong) return;
    onSend(trimmed);
    setText('');
  };

  // Grow the textarea with its content (up to max-h-48).
  useLayoutEffect(() => {
    const el = innerRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${el.scrollHeight}px`;
  }, [text]);

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      submit();
    }
  };

  return (
    <form
      className="relative flex items-end gap-2 rounded-2xl border border-slate-300 bg-white p-2 shadow-sm focus-within:border-teal-600 focus-within:ring-2 focus-within:ring-teal-600/20 dark:border-slate-700 dark:bg-slate-900"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <label htmlFor="composer" className="sr-only">
        Ask about a US stock
      </label>
      <textarea
        id="composer"
        ref={(el) => {
          innerRef.current = el;
          if (typeof ref === 'function') ref(el);
          else if (ref) ref.current = el;
        }}
        rows={1}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder={streaming ? 'Waiting for the reply…' : 'Ask about a US stock…'}
        className="max-h-48 min-h-10 flex-1 resize-none bg-transparent px-2 py-2 text-[15px] outline-none placeholder:text-slate-400 dark:placeholder:text-slate-500"
        aria-describedby="composer-hint"
        aria-invalid={tooLong}
      />
      {streaming ? (
        <button
          type="button"
          onClick={onStop}
          aria-label="Stop generating"
          className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-slate-800 text-white hover:bg-slate-700 dark:bg-slate-200 dark:text-slate-900 dark:hover:bg-white"
        >
          <IconStop />
        </button>
      ) : (
        <button
          type="submit"
          aria-label="Send message"
          disabled={!trimmed || tooLong}
          className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-teal-700 text-white hover:bg-teal-800 disabled:cursor-not-allowed disabled:opacity-40 dark:bg-teal-600 dark:hover:bg-teal-500"
        >
          <IconSend />
        </button>
      )}
      <span id="composer-hint" className="sr-only">
        Enter to send, Shift+Enter for a new line.
      </span>
      {tooLong && (
        <span className="absolute -top-6 right-2 text-xs text-rose-600">
          {text.length}/{MAX_CHARS} characters
        </span>
      )}
    </form>
  );
});
