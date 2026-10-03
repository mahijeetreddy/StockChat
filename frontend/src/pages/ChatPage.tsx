import { useCallback, useRef, useState, type ComponentType, type ReactNode } from 'react';
import { useChat } from '../state/useChat';
import { useTheme } from '../state/useTheme';
import { Composer } from '../components/chat/Composer';
import { EmptyState } from '../components/chat/EmptyState';
import { MessageList } from '../components/chat/MessageList';
import { Disclaimer } from '../components/layout/Disclaimer';
import { Header } from '../components/layout/Header';
import { Sidebar } from '../components/layout/Sidebar';

interface Props {
  /** Extra sidebar panels (watchlist, alerts). Receives a function to ask a question. */
  SidebarPanels?: ComponentType<{ ask: (text: string) => void }>;
  headerActions?: ReactNode;
}

export function ChatPage({ SidebarPanels, headerActions }: Props) {
  const { state, send, stop, newChat, load, updateConfirm } = useChat();
  const { theme, toggle } = useTheme();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const composerRef = useRef<HTMLTextAreaElement>(null);

  const ask = useCallback(
    (text: string) => {
      void send(text);
      composerRef.current?.focus();
    },
    [send],
  );

  const startNew = useCallback(() => {
    newChat();
    composerRef.current?.focus();
  }, [newChat]);

  return (
    <div className="flex h-dvh bg-slate-100/60 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
      <Sidebar
        open={sidebarOpen}
        onClose={() => setSidebarOpen(false)}
        activeId={state.conversationId}
        onSelect={(id) => void load(id)}
        onNewChat={startNew}
      >
        {SidebarPanels && <SidebarPanels ask={ask} />}
      </Sidebar>
      <div className="flex min-w-0 flex-1 flex-col">
        <Header
          theme={theme}
          onToggleTheme={toggle}
          onToggleSidebar={() => setSidebarOpen((o) => !o)}
          actions={headerActions}
        />
        <main className="flex min-h-0 flex-1 flex-col">
          {state.messages.length === 0 ? (
            <EmptyState onPick={ask} />
          ) : (
            <MessageList
              messages={state.messages}
              streaming={state.streaming}
              onRetry={ask}
              onConfirmUpdate={updateConfirm}
            />
          )}
          <div className="mx-auto w-full max-w-3xl px-4 pt-2">
            <Composer ref={composerRef} streaming={state.streaming} onSend={ask} onStop={stop} />
            <Disclaimer />
          </div>
        </main>
      </div>
    </div>
  );
}
