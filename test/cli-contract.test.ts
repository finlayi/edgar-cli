import { afterEach, describe, expect, it, vi } from 'vitest';

import { runCli } from '../src/cli.js';

interface IoCapture {
  stdout: string;
  stderr: string;
}

function buildIo(env: NodeJS.ProcessEnv = {}): {
  io: { stdout: (message: string) => void; stderr: (message: string) => void; env: NodeJS.ProcessEnv };
  capture: IoCapture;
} {
  const capture: IoCapture = { stdout: '', stderr: '' };

  return {
    io: {
      stdout: (message: string) => {
        capture.stdout += message;
      },
      stderr: (message: string) => {
        capture.stderr += message;
      },
      env
    },
    capture
  };
}

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: {
      'content-type': 'application/json'
    }
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('cli contract', () => {
  it('returns help with exit code 0', async () => {
    const { io, capture } = buildIo();

    const exitCode = await runCli(['--help'], io);

    expect(exitCode).toBe(0);
    expect(capture.stdout).toContain('Usage: edgar');
  });

  it('fails with IDENTITY_REQUIRED when user-agent is missing', async () => {
    const { io, capture } = buildIo();

    const exitCode = await runCli(['resolve', 'AAPL'], io);

    expect(exitCode).toBe(3);

    const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
    expect(payload.ok).toBe(false);
    expect((payload.error as { code: string }).code).toBe('IDENTITY_REQUIRED');
  });

  it('emits envelope for resolve command', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);

        if (url.endsWith('/files/company_tickers.json')) {
          return jsonResponse({
            '0': { cik_str: 320193, ticker: 'AAPL', title: 'Apple Inc.' }
          });
        }

        return jsonResponse({}, 404);
      })
    );

    const { io, capture } = buildIo();

    const exitCode = await runCli(['--user-agent', 'Name user@example.com', 'resolve', 'AAPL'], io);

    expect(exitCode).toBe(0);
    const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;

    expect(payload.ok).toBe(true);
    expect(payload.command).toBe('resolve');
    expect(payload.provider).toBe('sec');
    expect((payload.meta as Record<string, unknown>).output_schema).toBe('v1');
    expect((payload.data as Record<string, unknown>).cik).toBe('0000320193');
  });

  it('returns query metadata for filings list', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);

        if (url.endsWith('/files/company_tickers.json')) {
          return jsonResponse({
            '0': { cik_str: 320193, ticker: 'AAPL', title: 'Apple Inc.' }
          });
        }

        if (url.includes('/submissions/CIK0000320193.json')) {
          return jsonResponse({
            cik: '0000320193',
            filings: {
              recent: {
                accessionNumber: ['0000320193-26-000006', '0000320193-25-000111'],
                form: ['10-Q', '10-K'],
                filingDate: ['2026-01-30', '2025-10-31'],
                reportDate: ['2025-12-27', '2025-09-27'],
                primaryDocument: ['aapl-20251227.htm', 'aapl-20250927.htm']
              }
            }
          });
        }

        return jsonResponse({}, 404);
      })
    );

    const { io, capture } = buildIo();

    const exitCode = await runCli(
      [
        '--user-agent',
        'Name user@example.com',
        'filings',
        'list',
        '--id',
        'AAPL',
        '--form',
        '10-K',
        '--query-limit',
        '5'
      ],
      io
    );

    expect(exitCode).toBe(0);
    const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
    const meta = payload.meta as Record<string, unknown>;

    expect(payload.command).toBe('filings list');
    expect(meta.query_total_count).toBe(1);
    expect(meta.query_returned_count).toBe(1);
    expect(meta.query_offset).toBe(0);
  });

  it('returns canonical filing URL for filings get --format url', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);

        if (url.endsWith('/files/company_tickers.json')) {
          return jsonResponse({
            '0': { cik_str: 320193, ticker: 'AAPL', title: 'Apple Inc.' }
          });
        }

        if (url.includes('/submissions/CIK0000320193.json')) {
          return jsonResponse({
            cik: '0000320193',
            filings: {
              recent: {
                accessionNumber: ['0000320193-26-000006'],
                form: ['10-Q'],
                filingDate: ['2026-01-30'],
                reportDate: ['2025-12-27'],
                primaryDocument: ['aapl-20251227.htm']
              }
            }
          });
        }

        return jsonResponse({}, 404);
      })
    );

    const { io, capture } = buildIo();

    const exitCode = await runCli(
      [
        '--user-agent',
        'Name user@example.com',
        'filings',
        'get',
        '--id',
        'AAPL',
        '--accession',
        '0000320193-26-000006',
        '--format',
        'url'
      ],
      io
    );

    expect(exitCode).toBe(0);
    const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
    const data = payload.data as Record<string, unknown>;

    expect(data.url).toBe(
      'https://www.sec.gov/Archives/edgar/data/320193/000032019326000006/aapl-20251227.htm'
    );
  });

  it('returns latest company fact datapoint', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);

        if (url.endsWith('/files/company_tickers.json')) {
          return jsonResponse({
            '0': { cik_str: 320193, ticker: 'AAPL', title: 'Apple Inc.' }
          });
        }

        if (url.includes('/api/xbrl/companyfacts/CIK0000320193.json')) {
          return jsonResponse({
            cik: 320193,
            entityName: 'Apple Inc.',
            facts: {
              'us-gaap': {
                Revenues: {
                  label: 'Revenues',
                  units: {
                    USD: [
                      { filed: '2025-10-31', end: '2025-09-27', val: 100 },
                      { filed: '2026-01-30', end: '2025-12-27', val: 120 }
                    ]
                  }
                }
              }
            }
          });
        }

        return jsonResponse({}, 404);
      })
    );

    const { io, capture } = buildIo();

    const exitCode = await runCli(
      [
        '--user-agent',
        'Name user@example.com',
        'facts',
        'get',
        '--id',
        'AAPL',
        '--taxonomy',
        'us-gaap',
        '--concept',
        'Revenues',
        '--unit',
        'USD',
        '--latest'
      ],
      io
    );

    expect(exitCode).toBe(0);
    const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
    const data = payload.data as Record<string, unknown>;
    const latest = data.latest as Record<string, { val: number; filed: string }>;

    expect(latest.USD.val).toBe(120);
    expect(latest.USD.filed).toBe('2026-01-30');
  });
});
