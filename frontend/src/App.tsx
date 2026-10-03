import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState } from 'react';
import { NotificationBell, Toasts } from './components/layout/Notifications';
import { SidebarPanels } from './components/layout/SidebarPanels';
import { TokenGate } from './components/layout/TokenGate';
import { ChatPage } from './pages/ChatPage';
import { useNotifications } from './state/useNotifications';

function Shell() {
  const { items, toasts, unread, dismiss, markRead } = useNotifications();
  return (
    <>
      <ChatPage
        SidebarPanels={SidebarPanels}
        headerActions={<NotificationBell items={items} unread={unread} onOpen={markRead} />}
      />
      <Toasts toasts={toasts} onDismiss={dismiss} />
      <TokenGate />
    </>
  );
}

export default function App() {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false, staleTime: 5_000 } },
      }),
  );
  return (
    <QueryClientProvider client={client}>
      <Shell />
    </QueryClientProvider>
  );
}
