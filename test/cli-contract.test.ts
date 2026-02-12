import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
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

function researchSyncFixtureFetch(): typeof fetch {
  return vi.fn(async (input: RequestInfo | URL) => {
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
            accessionNumber: [
              '0000320193-26-000111',
              '0000320193-26-000112',
              '0000320193-25-000079',
              '0000320193-25-000210'
            ],
            form: ['8-K', '10-Q', '10-K', '8-K'],
            filingDate: ['2026-01-20', '2026-01-30', '2025-10-31', '2025-11-10'],
            reportDate: ['2026-01-20', '2025-12-27', '2025-09-27', '2025-11-10'],
            primaryDocument: [
              'aapl-20260120.htm',
              'aapl-20251227.htm',
              'aapl-20250927.htm',
              'aapl-20251110.htm'
            ]
          }
        }
      });
    }

    if (url.includes('/Archives/edgar/data/320193/000032019326000111/aapl-20260120.htm')) {
      return new Response(
        '<html><body><h2>Item 5.02</h2><p>Director resigned effective immediately.</p></body></html>',
        { status: 200, headers: { 'content-type': 'text/html' } }
      );
    }

    if (url.includes('/Archives/edgar/data/320193/000032019326000112/aapl-20251227.htm')) {
      return new Response(
        '<html><body><h2>Item 2</h2><p>Management discussion indicates revenue growth.</p></body></html>',
        { status: 200, headers: { 'content-type': 'text/html' } }
      );
    }

    if (url.includes('/Archives/edgar/data/320193/000032019325000079/aapl-20250927.htm')) {
      return new Response(
        '<html><body><h2>Item 1A Risk Factors</h2><p>Supply chain and macroeconomic risks.</p></body></html>',
        { status: 200, headers: { 'content-type': 'text/html' } }
      );
    }

    if (url.includes('/Archives/edgar/data/320193/000032019325000210/aapl-20251110.htm')) {
      return new Response(
        '<html><body><h2>Item 8.01</h2><p>Product launch event update.</p></body></html>',
        { status: 200, headers: { 'content-type': 'text/html' } }
      );
    }

    return jsonResponse({}, 404);
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

  it('returns readable filing text for filings get --format text', async () => {
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

        if (url.includes('/Archives/edgar/data/320193/000032019326000006/aapl-20251227.htm')) {
          return new Response(
            '<html><body><ix:hidden><div>SHOULD_NOT_APPEAR</div></ix:hidden><div>Alpha</div><div>Beta<span>Gamma</span></div><p>Delta</p></body></html>',
            { status: 200, headers: { 'content-type': 'text/html' } }
          );
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
        'text'
      ],
      io
    );

    expect(exitCode).toBe(0);
    const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
    const data = payload.data as Record<string, string>;

    expect(data.content).toContain('Alpha');
    expect(data.content).toContain('BetaGamma');
    expect(data.content).toContain('Delta');
    expect(data.content).not.toContain('SHOULD_NOT_APPEAR');
  });

  it('returns markdown for filings get --format markdown', async () => {
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

        if (url.includes('/Archives/edgar/data/320193/000032019326000006/aapl-20251227.htm')) {
          return new Response(
            '<html><body><h2>Highlights</h2><table><thead><tr><th>Name</th><th>Value</th></tr></thead><tbody><tr><td>Revenue</td><td>$1</td></tr></tbody></table></body></html>',
            { status: 200, headers: { 'content-type': 'text/html' } }
          );
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
        'markdown'
      ],
      io
    );

    expect(exitCode).toBe(0);
    const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
    const data = payload.data as Record<string, string>;

    expect(data.content).toContain('## Highlights');
    expect(data.content).toContain('| Name | Value |');
    expect(data.content).toContain('| --- | --- |');
  });

  it('returns DOCS_REQUIRED when research ask has no docs', async () => {
    const { io, capture } = buildIo();

    const exitCode = await runCli(['research', 'ask', 'chip revenue trends'], io);

    expect(exitCode).toBe(2);
    const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
    expect(payload.ok).toBe(false);
    expect((payload.error as { code: string }).code).toBe('DOCS_REQUIRED');
  });

  it('requires identity for research ask when --id is provided', async () => {
    const { io, capture } = buildIo();

    const exitCode = await runCli(['research', 'ask', 'board resignation', '--id', 'AAPL'], io);

    expect(exitCode).toBe(3);
    const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
    expect(payload.ok).toBe(false);
    expect((payload.error as { code: string }).code).toBe('IDENTITY_REQUIRED');
  });

  it('runs research ask with explicit docs and lexical provenance', async () => {
    const tempDir = await mkdtemp(path.join(os.tmpdir(), 'edgar-cli-test-'));
    const docPathA = path.join(tempDir, 'nvda-8k.md');
    const docPathB = path.join(tempDir, 'aapl-10k.md');
    const manifestPath = path.join(tempDir, 'docs.json');

    try {
      await writeFile(
        docPathA,
        [
          '# Item 5.02',
          'Persis Drell resigned from the Board effective immediately.',
          'No disagreement with company operations.',
          ''
        ].join('\n'),
        'utf8'
      );

      await writeFile(
        docPathB,
        [
          '# Item 7',
          'Management discussion includes net sales and gross margin analysis.',
          'Risk factors are discussed in Item 1A.',
          ''
        ].join('\n'),
        'utf8'
      );

      await writeFile(manifestPath, JSON.stringify({ docs: [docPathB] }), 'utf8');

      const { io, capture } = buildIo();

      const exitCode = await runCli(
        [
          'research',
          'ask',
          'board resigned effective immediately',
          '--doc',
          docPathA,
          '--manifest',
          manifestPath,
          '--top-k',
          '3',
          '--chunk-lines',
          '20',
          '--chunk-overlap',
          '5'
        ],
        io
      );

      expect(exitCode).toBe(0);
      const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
      expect(payload.ok).toBe(true);
      expect(payload.command).toBe('research ask');

      const data = payload.data as {
        backend: string;
        result_count: number;
        results: Array<{
          path: string;
          line_start: number;
          line_end: number;
          score: number;
          excerpt: string;
        }>;
      };

      expect(data.backend).toBe('lexical');
      expect(data.result_count).toBeGreaterThan(0);
      expect(data.results[0].path).toBe(docPathA);
      expect(data.results[0].line_start).toBeGreaterThan(0);
      expect(data.results[0].line_end).toBeGreaterThanOrEqual(data.results[0].line_start);
      expect(data.results[0].score).toBeGreaterThan(0);
      expect(data.results[0].excerpt.toLowerCase()).toContain('resigned');
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it('downranks filing cover boilerplate for broad guidance queries', async () => {
    const tempDir = await mkdtemp(path.join(os.tmpdir(), 'edgar-cli-test-'));
    const docPath = path.join(tempDir, 'msft-10q.md');

    try {
      await writeFile(
        docPath,
        [
          'For the quarterly period ended December 31, 2025',
          'Securities registered pursuant to Section 12(b) of the Act.',
          'Indicate by check mark whether the registrant has filed all required reports.',
          '| Title of each class | Trading Symbol | Name of exchange |',
          '| --- | --- | --- |',
          '| Common stock | MSFT | Nasdaq |',
          '',
          'Management updated quarterly guidance for cloud gross margin.',
          'The company changed guidance after stronger-than-expected AI demand.',
          'Revenue outlook for the latest quarter increased.',
          ''
        ].join('\n'),
        'utf8'
      );

      const { io, capture } = buildIo();
      const exitCode = await runCli(
        [
          'research',
          'ask',
          'what changed in the latest quarter guidance',
          '--doc',
          docPath,
          '--top-k',
          '1',
          '--chunk-lines',
          '6',
          '--chunk-overlap',
          '0'
        ],
        io
      );

      expect(exitCode).toBe(0);
      const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
      const data = payload.data as {
        query_terms: string[];
        result_count: number;
        results: Array<{ excerpt: string }>;
      };

      expect(data.query_terms).not.toContain('what');
      expect(data.query_terms).not.toContain('the');
      expect(data.result_count).toBeGreaterThan(0);
      expect(data.results[0].excerpt.toLowerCase()).toContain('guidance');
      expect(data.results[0].excerpt.toLowerCase()).toContain('changed');
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it('syncs deterministic profile docs to cache', async () => {
    vi.stubGlobal('fetch', researchSyncFixtureFetch());

    const tempDir = await mkdtemp(path.join(os.tmpdir(), 'edgar-cli-cache-'));
    try {
      const { io, capture } = buildIo();
      const exitCode = await runCli(
        [
          '--user-agent',
          'Name user@example.com',
          'research',
          'sync',
          '--id',
          'AAPL',
          '--profile',
          'core',
          '--cache-dir',
          tempDir
        ],
        io
      );

      expect(exitCode).toBe(0);
      const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
      const data = payload.data as {
        docs_count: number;
        fetched_count: number;
        reused_count: number;
        docs: Array<{ path: string }>;
        manifest_path: string;
      };

      expect(data.docs_count).toBe(4);
      expect(data.fetched_count).toBe(4);
      expect(data.reused_count).toBe(0);
      expect(data.docs.length).toBe(4);

      const firstDocContent = await readFile(data.docs[0].path, 'utf8');
      expect(firstDocContent.length).toBeGreaterThan(10);

      const manifestContent = await readFile(data.manifest_path, 'utf8');
      expect(manifestContent).toContain('"profile": "core"');
      expect(manifestContent).toContain('"docs"');
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it('runs research ask with --id using cached/synced docs', async () => {
    vi.stubGlobal('fetch', researchSyncFixtureFetch());

    const tempDir = await mkdtemp(path.join(os.tmpdir(), 'edgar-cli-cache-ask-'));
    try {
      const { io, capture } = buildIo();

      const exitCode = await runCli(
        [
          '--user-agent',
          'Name user@example.com',
          'research',
          'ask',
          'who resigned effective immediately',
          '--id',
          'AAPL',
          '--profile',
          'core',
          '--cache-dir',
          tempDir,
          '--top-k',
          '3'
        ],
        io
      );

      expect(exitCode).toBe(0);
      const payload = JSON.parse(capture.stdout.trim()) as Record<string, unknown>;
      expect(payload.ok).toBe(true);
      expect(payload.command).toBe('research ask');

      const data = payload.data as {
        backend: string;
        profile: string;
        corpus_docs_count: number;
        result_count: number;
        results: Array<{ excerpt: string }>;
      };

      expect(data.backend).toBe('lexical');
      expect(data.profile).toBe('core');
      expect(data.corpus_docs_count).toBeGreaterThan(0);
      expect(data.result_count).toBeGreaterThan(0);
      expect(data.results[0].excerpt.toLowerCase()).toContain('resigned');
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
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
