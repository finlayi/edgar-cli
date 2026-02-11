import { CLIError, ErrorCode } from '../core/errors.js';
import { tickerMapUrl } from './endpoints.js';
import { isLikelyCik, normalizeCik, normalizeTicker } from './normalizers.js';
import { SecClient } from './client.js';

export interface TickerRecord {
  cik_str: number;
  ticker: string;
  title: string;
}

export interface ResolvedEntity {
  input: string;
  cik: string;
  cik_numeric: number;
  ticker: string | null;
  title: string | null;
}

let cachedMap: TickerRecord[] | null = null;
let cachedAtMs = 0;
const CACHE_TTL_MS = 15 * 60 * 1000;

async function getTickerMap(client: SecClient): Promise<TickerRecord[]> {
  const now = Date.now();
  if (cachedMap && now - cachedAtMs < CACHE_TTL_MS) {
    return cachedMap;
  }

  const payload = await client.fetchSecJson<Record<string, TickerRecord>>(tickerMapUrl());
  const records = Object.values(payload)
    .filter((record) => record && typeof record === 'object')
    .filter((record) => typeof record.cik_str === 'number' && typeof record.ticker === 'string');

  cachedMap = records;
  cachedAtMs = now;

  return records;
}

function findByTicker(records: TickerRecord[], ticker: string): TickerRecord | undefined {
  return records.find((record) => record.ticker.toUpperCase() === ticker);
}

function findByCik(records: TickerRecord[], cik10: string): TickerRecord | undefined {
  const cikNumeric = Number.parseInt(cik10, 10);
  return records.find((record) => record.cik_str === cikNumeric);
}

export async function resolveEntity(
  id: string,
  client: SecClient,
  options?: { strictMapMatch?: boolean }
): Promise<ResolvedEntity> {
  const strictMapMatch = options?.strictMapMatch ?? false;
  const records = await getTickerMap(client);

  if (isLikelyCik(id)) {
    const cik = normalizeCik(id);
    const cikNumeric = Number.parseInt(cik, 10);
    const match = findByCik(records, cik);

    if (!match && strictMapMatch) {
      throw new CLIError(ErrorCode.NOT_FOUND, `No SEC ticker-map record found for CIK ${cik}`);
    }

    return {
      input: id,
      cik,
      cik_numeric: cikNumeric,
      ticker: match?.ticker ?? null,
      title: match?.title ?? null
    };
  }

  const ticker = normalizeTicker(id);
  const match = findByTicker(records, ticker);

  if (!match) {
    throw new CLIError(ErrorCode.NOT_FOUND, `No SEC ticker-map record found for ticker ${ticker}`);
  }

  return {
    input: id,
    cik: String(match.cik_str).padStart(10, '0'),
    cik_numeric: match.cik_str,
    ticker: match.ticker,
    title: match.title
  };
}
