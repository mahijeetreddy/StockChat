import { render, screen } from '@testing-library/react';
import App from './App';

describe('App', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        const body = url.includes('/api/conversations')
          ? { conversations: [] }
          : url.includes('/api/market/status')
            ? { open: false, as_of: '2026-10-03T12:00:00Z' }
            : url.includes('/api/watchlist')
              ? { symbols: ['TSLA'], quotes: [] }
              : url.includes('/api/alerts')
                ? { alerts: [] }
                : {};
        return new Response(JSON.stringify(body), {
          headers: { 'Content-Type': 'application/json' },
        });
      }),
    );
    vi.stubGlobal('matchMedia', () => ({ matches: false }));
  });
  afterEach(() => vi.unstubAllGlobals());

  it('renders the shell with suggestions and the disclaimer', async () => {
    render(<App />);
    expect(screen.getByRole('heading', { name: 'StockChat' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: "What's Apple trading at?" })).toBeInTheDocument();
    expect(screen.getByText(/not financial advice/i)).toBeInTheDocument();
    expect(await screen.findByTitle('US market is closed')).toBeInTheDocument();
    expect(await screen.findByText('TSLA')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Notifications' })).toBeInTheDocument();
  });
});
