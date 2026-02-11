import { describe, expect, it } from 'vitest';

import { filingDocumentUrl } from '../src/sec/endpoints.js';
import { normalizeAccession, normalizeCik, normalizeTicker } from '../src/sec/normalizers.js';

describe('normalizers', () => {
  it('normalizes cik to 10 digits', () => {
    expect(normalizeCik('320193')).toBe('0000320193');
    expect(normalizeCik('0000320193')).toBe('0000320193');
  });

  it('normalizes ticker to uppercase', () => {
    expect(normalizeTicker('aapl')).toBe('AAPL');
  });

  it('normalizes accession format', () => {
    expect(normalizeAccession('0000320193-26-000006')).toBe('0000320193-26-000006');
  });

  it('builds filing url from cik/accession/primary doc', () => {
    const url = filingDocumentUrl({
      cik: '0000320193',
      accession: '0000320193-26-000006',
      primaryDocument: 'aapl-20251227.htm'
    });

    expect(url).toBe(
      'https://www.sec.gov/Archives/edgar/data/320193/000032019326000006/aapl-20251227.htm'
    );
  });
});
