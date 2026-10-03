import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { ConfirmCard } from './ConfirmCard';
import { QuoteCard } from './QuoteCard';
import { NewsList } from './NewsList';
import { ProfileCard } from './ProfileCard';
import type { ConfirmData, Quote } from '../../lib/types';

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const quote: Quote = {
  symbol: 'AAPL',
  price: 1234.5,
  change: -12.3,
  change_percent: -0.99,
  high: 1250,
  low: 1200,
  open: 1240,
  prev_close: 1246.8,
  as_of: '2026-10-02T20:00:00Z',
  market_open: false,
};

describe('QuoteCard', () => {
  it('formats price and change, with a non-color direction cue', () => {
    render(<QuoteCard quote={quote} />);
    expect(screen.getByText('$1,234.50')).toBeInTheDocument();
    expect(screen.getByText('−12.30')).toBeInTheDocument();
    expect(screen.getByText('(−0.99%)')).toBeInTheDocument();
    expect(screen.getByText('▼')).toBeInTheDocument();
  });

  it('flags a closed market and labels the last close', () => {
    render(<QuoteCard quote={quote} />);
    expect(screen.getByText('Market closed')).toBeInTheDocument();
    expect(screen.getByText('Last close')).toBeInTheDocument();
  });

  it('shows "As of" while the market is open', () => {
    render(<QuoteCard quote={{ ...quote, market_open: true, change: 1, change_percent: 0.1 }} />);
    expect(screen.queryByText('Market closed')).not.toBeInTheDocument();
    expect(screen.getByText('As of')).toBeInTheDocument();
    expect(screen.getByText('▲')).toBeInTheDocument();
  });
});

const pending: ConfirmData = {
  action_id: 'act_1',
  call_id: 'c1',
  summary: 'Alert when AAPL rises above $250.00, one time',
  tool: 'create_alert',
  input: {},
  status: 'pending',
};

describe('ConfirmCard', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('confirms and reports the result', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({ action_id: 'act_1', status: 'done', result: 'Alert #1 created' }),
    );
    vi.stubGlobal('fetch', fetchMock);
    const onUpdate = vi.fn();
    wrap(<ConfirmCard confirm={pending} onUpdate={onUpdate} />);
    expect(screen.getByText('Waiting for your confirmation')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /confirm/i }));
    await waitFor(() => expect(onUpdate).toHaveBeenCalledWith('act_1', 'done', 'Alert #1 created'));
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/actions/act_1/confirm',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('uses the server status on 409 (already handled or expired)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        jsonResponse({ error: 'This action expired.', status: 'expired', result: '' }, 409),
      ),
    );
    const onUpdate = vi.fn();
    wrap(<ConfirmCard confirm={pending} onUpdate={onUpdate} />);
    await userEvent.click(screen.getByRole('button', { name: /cancel/i }));
    await waitFor(() => expect(onUpdate).toHaveBeenCalledWith('act_1', 'expired', ''));
  });

  it('shows an error and keeps the buttons on a server failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('oops', { status: 500 })),
    );
    wrap(<ConfirmCard confirm={pending} onUpdate={vi.fn()} />);
    await userEvent.click(screen.getByRole('button', { name: /confirm/i }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Request failed (500)');
    expect(screen.getByRole('button', { name: /confirm/i })).toBeEnabled();
  });

  it('has no buttons once handled', () => {
    wrap(
      <ConfirmCard
        confirm={{ ...pending, status: 'done', result: 'Alert #1 created' }}
        onUpdate={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
    expect(screen.getByText('Confirmed')).toBeInTheDocument();
    expect(screen.getByText('Alert #1 created')).toBeInTheDocument();
  });
});

describe('NewsList', () => {
  it('renders untrusted text as plain text with safe links', () => {
    render(
      <NewsList
        data={{
          symbol: 'MSFT',
          items: [
            {
              headline: '<img src=x onerror=alert(1)> Big news',
              summary: 'Summary',
              source: 'Wire',
              url: 'https://example.com/a',
              published_at: new Date().toISOString(),
            },
          ],
        }}
      />,
    );
    const link = screen.getByRole('link');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer nofollow');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveTextContent('<img src=x onerror=alert(1)> Big news');
    expect(document.querySelector('img')).toBeNull();
  });
});

describe('ProfileCard', () => {
  it('formats market cap compactly and falls back to initials', () => {
    render(
      <ProfileCard
        profile={{
          symbol: 'COST',
          name: 'Costco Wholesale Corp',
          exchange: 'NASDAQ',
          industry: 'Retail',
          country: 'US',
          currency: 'USD',
          web_url: 'https://www.costco.com/',
          logo_url: '',
          market_cap: 4e11,
        }}
      />,
    );
    expect(screen.getByText('$400B')).toBeInTheDocument();
    expect(screen.getByText('CO')).toBeInTheDocument();
    expect(screen.getByRole('link')).toHaveTextContent('costco.com');
  });
});
