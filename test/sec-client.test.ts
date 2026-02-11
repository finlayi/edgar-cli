import { describe, expect, it, vi } from 'vitest';

import { CLIError, ErrorCode } from '../src/core/errors.js';
import { SecClient } from '../src/sec/client.js';

function jsonResponse(payload: unknown, status = 200, headers?: Record<string, string>): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: {
      'content-type': 'application/json',
      ...(headers ?? {})
    }
  });
}

describe('sec client', () => {
  it('retries 429 with retry-after and succeeds', async () => {
    const randomSpy = vi.spyOn(Math, 'random').mockReturnValue(0);
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({ message: 'rate limited' }, 429, { 'retry-after': '0' }))
      .mockResolvedValueOnce(jsonResponse({ ok: true }, 200));

    const client = new SecClient({ userAgent: 'Name user@example.com', fetchImpl });

    const result = await client.fetchSecJson<{ ok: boolean }>('https://data.sec.gov/test.json');

    expect(result).toEqual({ ok: true });
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    randomSpy.mockRestore();
  });

  it('maps 403 undeclared tool page to IDENTITY_REQUIRED', async () => {
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(
      new Response('<html><h1>Undeclared Automated Tool</h1></html>', { status: 403 })
    );

    const client = new SecClient({ userAgent: 'Name user@example.com', fetchImpl });

    await expect(client.fetchSecJson('https://data.sec.gov/test.json')).rejects.toMatchObject<Partial<CLIError>>({
      code: ErrorCode.IDENTITY_REQUIRED
    });
  });
});
