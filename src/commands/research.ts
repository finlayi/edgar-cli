import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';

import { runFilingsGet, runFilingsList } from './filings.js';
import { CLIError, ErrorCode } from '../core/errors.js';
import { CommandContext, CommandResult } from '../core/runtime.js';
import { resolveEntity } from '../sec/ticker-map.js';

type ResearchProfile = 'core' | 'events' | 'financials';

interface SyncRule {
  form: string;
  queryLimit: number;
  recentDays?: number;
}

interface CachedDoc {
  accession: string;
  form: string | null;
  filing_date: string | null;
  report_date: string | null;
  filing_url: string | null;
  path: string;
}

interface CachedManifest {
  version: 1;
  id_input: string;
  cik: string;
  ticker: string | null;
  title: string | null;
  profile: ResearchProfile;
  synced_at: string;
  docs: CachedDoc[];
}

interface FilingRow {
  accession: string;
  form: string | null;
  filingDate: string | null;
  reportDate: string | null;
  filingUrl: string | null;
}

const PROFILE_RULES: Record<ResearchProfile, SyncRule[]> = {
  core: [
    { form: '10-K', queryLimit: 1 },
    { form: '10-Q', queryLimit: 3 },
    { form: '8-K', queryLimit: 12, recentDays: 180 }
  ],
  events: [{ form: '8-K', queryLimit: 24, recentDays: 365 }],
  financials: [
    { form: '10-K', queryLimit: 2 },
    { form: '10-Q', queryLimit: 6 }
  ]
};

interface Chunk {
  docPath: string;
  accession: string | null;
  lineStart: number;
  lineEnd: number;
  text: string;
  tokenCount: number;
  termFrequency: Map<string, number>;
}

interface ParsedManifest {
  docs: string[];
}

function nowIso(): string {
  return new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
}

function formatDateUtc(date: Date): string {
  return date.toISOString().slice(0, 10);
}

function dateDaysAgo(days: number): string {
  const date = new Date();
  date.setUTCDate(date.getUTCDate() - days);
  return formatDateUtc(date);
}

function defaultCacheRoot(): string {
  if (process.env.EDGAR_CACHE_DIR && process.env.EDGAR_CACHE_DIR.trim().length > 0) {
    return path.resolve(process.env.EDGAR_CACHE_DIR);
  }

  if (process.env.XDG_CACHE_HOME && process.env.XDG_CACHE_HOME.trim().length > 0) {
    return path.resolve(process.env.XDG_CACHE_HOME, 'edgar-cli');
  }

  return path.resolve(os.homedir(), '.cache', 'edgar-cli');
}

function resolveCacheRoot(cacheDir?: string): string {
  if (cacheDir && cacheDir.trim().length > 0) {
    return path.resolve(cacheDir);
  }
  return defaultCacheRoot();
}

function companyCacheDir(cacheRoot: string, cik: string): string {
  return path.join(cacheRoot, 'research', 'companies', cik);
}

function profileManifestPath(cacheRoot: string, cik: string, profile: ResearchProfile): string {
  return path.join(companyCacheDir(cacheRoot, cik), 'profiles', `${profile}.json`);
}

function filingDocPath(cacheRoot: string, cik: string, accession: string): string {
  return path.join(companyCacheDir(cacheRoot, cik), 'filings', `${accession}.md`);
}

function parseCachedManifest(value: unknown): CachedManifest {
  if (!value || typeof value !== 'object') {
    throw new CLIError(ErrorCode.PARSE_ERROR, 'Cached manifest is malformed');
  }

  const manifest = value as CachedManifest;
  if (
    manifest.version !== 1 ||
    typeof manifest.cik !== 'string' ||
    !Array.isArray(manifest.docs) ||
    !manifest.docs.every((doc) => doc && typeof doc.path === 'string' && typeof doc.accession === 'string')
  ) {
    throw new CLIError(ErrorCode.PARSE_ERROR, 'Cached manifest is malformed');
  }

  return manifest;
}

async function readCachedManifest(
  cacheRoot: string,
  cik: string,
  profile: ResearchProfile
): Promise<CachedManifest | null> {
  const manifestPath = profileManifestPath(cacheRoot, cik, profile);
  let raw: string;
  try {
    raw = await readFile(manifestPath, 'utf8');
  } catch (error) {
    const err = error as NodeJS.ErrnoException;
    if (err.code === 'ENOENT') {
      return null;
    }

    throw new CLIError(
      ErrorCode.VALIDATION_ERROR,
      `Unable to read cached manifest ${manifestPath}: ${err.message}`
    );
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw) as unknown;
  } catch {
    throw new CLIError(ErrorCode.PARSE_ERROR, `Cached manifest is not valid JSON: ${manifestPath}`);
  }

  return parseCachedManifest(parsed);
}

async function writeCachedManifest(
  cacheRoot: string,
  manifest: CachedManifest
): Promise<{ manifestPath: string }> {
  const manifestPath = profileManifestPath(cacheRoot, manifest.cik, manifest.profile);
  await mkdir(path.dirname(manifestPath), { recursive: true });
  await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, 'utf8');
  return { manifestPath };
}

async function fileExists(filePath: string): Promise<boolean> {
  try {
    const fileStat = await stat(filePath);
    return fileStat.isFile();
  } catch (error) {
    const err = error as NodeJS.ErrnoException;
    if (err.code === 'ENOENT') {
      return false;
    }

    throw new CLIError(ErrorCode.VALIDATION_ERROR, `Unable to stat ${filePath}: ${err.message}`);
  }
}

export function parseResearchProfile(value: string): ResearchProfile {
  const normalized = value.trim().toLowerCase();
  if (normalized === 'core' || normalized === 'events' || normalized === 'financials') {
    return normalized;
  }

  throw new CLIError(
    ErrorCode.VALIDATION_ERROR,
    '--profile must be one of core|events|financials'
  );
}

function tokenize(value: string): string[] {
  return (value.toLowerCase().match(/[a-z0-9]+/g) ?? []).filter((token) => token.length >= 2);
}

const QUERY_STOPWORDS = new Set([
  'a',
  'an',
  'and',
  'are',
  'as',
  'at',
  'be',
  'by',
  'for',
  'from',
  'how',
  'in',
  'into',
  'is',
  'it',
  'its',
  'of',
  'on',
  'or',
  'that',
  'the',
  'their',
  'there',
  'these',
  'they',
  'this',
  'to',
  'was',
  'were',
  'what',
  'when',
  'where',
  'which',
  'who',
  'why',
  'with'
]);

const COVER_BOILERPLATE_PATTERNS = [
  /securities registered pursuant to section 12\(b\)/i,
  /indicate by check mark/i,
  /commission file number/i,
  /for the quarterly period ended/i,
  /for the fiscal year ended/i
];

function uniqueTokens(tokens: string[]): string[] {
  return [...new Set(tokens)];
}

function buildQueryTerms(query: string): string[] {
  const rawTokens = tokenize(query);
  const filtered = rawTokens.filter((token) => !QUERY_STOPWORDS.has(token));
  const terms = filtered.length > 0 ? filtered : rawTokens;
  return uniqueTokens(terms);
}

function buildQueryBigrams(queryTerms: string[]): string[] {
  const bigrams: string[] = [];
  for (let idx = 0; idx < queryTerms.length - 1; idx += 1) {
    bigrams.push(`${queryTerms[idx]} ${queryTerms[idx + 1]}`);
  }
  return uniqueTokens(bigrams);
}

function countTermHits(queryTerms: string[], termFrequency: Map<string, number>): number {
  return queryTerms.reduce(
    (hits, term) => hits + ((termFrequency.get(term) ?? 0) > 0 ? 1 : 0),
    0
  );
}

function countBigramHits(chunkText: string, queryBigrams: string[]): number {
  if (queryBigrams.length === 0) {
    return 0;
  }

  const text = chunkText.toLowerCase();
  return queryBigrams.reduce((hits, bigram) => hits + (text.includes(bigram) ? 1 : 0), 0);
}

function looksLikeCoverBoilerplate(chunk: Chunk): boolean {
  if (chunk.lineStart > 140) {
    return false;
  }

  return COVER_BOILERPLATE_PATTERNS.some((pattern) => pattern.test(chunk.text));
}

function buildTermFrequency(tokens: string[]): Map<string, number> {
  const frequency = new Map<string, number>();
  for (const token of tokens) {
    frequency.set(token, (frequency.get(token) ?? 0) + 1);
  }
  return frequency;
}

function extractAccession(docPath: string): string | null {
  const match = docPath.match(/\d{10}-\d{2}-\d{6}/);
  return match?.[0] ?? null;
}

function parseManifest(value: unknown): ParsedManifest {
  if (Array.isArray(value) && value.every((entry) => typeof entry === 'string')) {
    return { docs: value };
  }

  if (
    value &&
    typeof value === 'object' &&
    Array.isArray((value as { docs?: unknown }).docs) &&
    (value as { docs: unknown[] }).docs.every((entry) => typeof entry === 'string')
  ) {
    return { docs: (value as { docs: string[] }).docs };
  }

  throw new CLIError(
    ErrorCode.VALIDATION_ERROR,
    'Manifest must be a JSON array of strings or object with a docs string array'
  );
}

async function loadDocPaths(params: {
  docs: string[];
  manifestPath?: string;
}): Promise<string[]> {
  const fromOptions = params.docs.map((docPath) => docPath.trim()).filter((docPath) => docPath.length > 0);
  const fromManifest: string[] = [];

  if (params.manifestPath) {
    const resolvedManifestPath = path.resolve(params.manifestPath);
    let manifestRaw: string;
    try {
      manifestRaw = await readFile(resolvedManifestPath, 'utf8');
    } catch (error) {
      const err = error as NodeJS.ErrnoException;
      if (err.code === 'ENOENT') {
        throw new CLIError(ErrorCode.NOT_FOUND, `Manifest not found: ${resolvedManifestPath}`);
      }
      throw new CLIError(
        ErrorCode.VALIDATION_ERROR,
        `Unable to read manifest ${resolvedManifestPath}: ${err.message}`
      );
    }

    let manifestJson: unknown;
    try {
      manifestJson = JSON.parse(manifestRaw) as unknown;
    } catch {
      throw new CLIError(
        ErrorCode.VALIDATION_ERROR,
        `Manifest is not valid JSON: ${resolvedManifestPath}`
      );
    }

    const parsed = parseManifest(manifestJson);
    fromManifest.push(...parsed.docs.map((docPath) => docPath.trim()).filter((docPath) => docPath.length > 0));
  }

  const resolved = [...fromOptions, ...fromManifest].map((docPath) => path.resolve(docPath));
  return [...new Set(resolved)];
}

async function ensureReadableTextFile(filePath: string): Promise<string> {
  let fileStat;
  try {
    fileStat = await stat(filePath);
  } catch (error) {
    const err = error as NodeJS.ErrnoException;
    if (err.code === 'ENOENT') {
      throw new CLIError(ErrorCode.NOT_FOUND, `Document not found: ${filePath}`);
    }
    throw new CLIError(ErrorCode.VALIDATION_ERROR, `Unable to stat document ${filePath}: ${err.message}`);
  }

  if (!fileStat.isFile()) {
    throw new CLIError(ErrorCode.VALIDATION_ERROR, `Path is not a file: ${filePath}`);
  }

  let content: string;
  try {
    content = await readFile(filePath, 'utf8');
  } catch (error) {
    const err = error as NodeJS.ErrnoException;
    throw new CLIError(ErrorCode.VALIDATION_ERROR, `Unable to read document ${filePath}: ${err.message}`);
  }

  if (content.includes('\u0000')) {
    throw new CLIError(ErrorCode.VALIDATION_ERROR, `File appears to be binary: ${filePath}`);
  }

  return content;
}

function chunkDocument(params: {
  docPath: string;
  content: string;
  chunkLines: number;
  chunkOverlap: number;
}): Chunk[] {
  const lines = params.content.split(/\r?\n/);
  const step = Math.max(1, params.chunkLines - params.chunkOverlap);
  const chunks: Chunk[] = [];
  const accession = extractAccession(params.docPath);

  for (let lineIdx = 0; lineIdx < lines.length; lineIdx += step) {
    const start = lineIdx;
    const endExclusive = Math.min(lines.length, start + params.chunkLines);
    const chunkLines = lines.slice(start, endExclusive);
    const text = chunkLines.join('\n').trim();
    if (text.length === 0) {
      if (endExclusive >= lines.length) {
        break;
      }
      continue;
    }

    const tokens = tokenize(text);
    chunks.push({
      docPath: params.docPath,
      accession,
      lineStart: start + 1,
      lineEnd: endExclusive,
      text,
      tokenCount: tokens.length,
      termFrequency: buildTermFrequency(tokens)
    });

    if (endExclusive >= lines.length) {
      break;
    }
  }

  return chunks;
}

function bm25Score(params: {
  queryTerms: string[];
  chunk: Chunk;
  docFrequencyByTerm: Map<string, number>;
  totalChunkCount: number;
  averageChunkLength: number;
}): number {
  const k1 = 1.2;
  const b = 0.75;

  return params.queryTerms.reduce((score, term) => {
    const tf = params.chunk.termFrequency.get(term) ?? 0;
    if (tf === 0) {
      return score;
    }

    const df = params.docFrequencyByTerm.get(term) ?? 0;
    const idf = Math.log(1 + (params.totalChunkCount - df + 0.5) / (df + 0.5));
    const normalizedLength = params.averageChunkLength > 0 ? params.chunk.tokenCount / params.averageChunkLength : 1;
    const denominator = tf + k1 * (1 - b + b * normalizedLength);
    const termScore = idf * ((tf * (k1 + 1)) / denominator);
    return score + termScore;
  }, 0);
}

function adjustedChunkScore(params: {
  chunk: Chunk;
  baseScore: number;
  queryTerms: string[];
  queryBigrams: string[];
}): number {
  if (params.baseScore <= 0) {
    return 0;
  }

  const termHits = countTermHits(params.queryTerms, params.chunk.termFrequency);
  if (params.queryTerms.length >= 3 && termHits < 2) {
    return 0;
  }

  const coverage = termHits / Math.max(1, params.queryTerms.length);
  const bigramHits = countBigramHits(params.chunk.text, params.queryBigrams);

  let multiplier = 1;

  if (coverage >= 1) {
    multiplier *= 1.25;
  } else if (coverage >= 0.7) {
    multiplier *= 1.15;
  } else if (coverage >= 0.5) {
    multiplier *= 1.08;
  } else if (params.queryTerms.length >= 3 && coverage <= 0.25) {
    multiplier *= 0.8;
  }

  if (bigramHits > 0) {
    multiplier *= 1 + Math.min(0.24, bigramHits * 0.08);
  }

  if (looksLikeCoverBoilerplate(params.chunk)) {
    multiplier *= 0.45;
  }

  return params.baseScore * multiplier;
}

function compactWhitespace(value: string): string {
  return value.replace(/[ \t]+/g, ' ').replace(/\n{3,}/g, '\n\n').trim();
}

function trimExcerpt(value: string, maxChars: number): string {
  if (value.length <= maxChars) {
    return value;
  }

  return `${value.slice(0, Math.max(0, maxChars - 3)).trimEnd()}...`;
}

async function runLexicalSearch(params: {
  query: string;
  docPaths: string[];
  topK: number;
  chunkLines: number;
  chunkOverlap: number;
}): Promise<CommandResult> {
  const query = params.query.trim();
  if (query.length === 0) {
    throw new CLIError(ErrorCode.VALIDATION_ERROR, 'Query must not be empty');
  }

  if (params.chunkOverlap >= params.chunkLines) {
    throw new CLIError(ErrorCode.VALIDATION_ERROR, '--chunk-overlap must be less than --chunk-lines');
  }

  const docs = await Promise.all(
    params.docPaths.map(async (docPath) => {
      const content = await ensureReadableTextFile(docPath);
      return {
        path: docPath,
        bytes: Buffer.byteLength(content, 'utf8'),
        lineCount: content.split(/\r?\n/).length,
        chunks: chunkDocument({
          docPath,
          content,
          chunkLines: params.chunkLines,
          chunkOverlap: params.chunkOverlap
        })
      };
    })
  );

  const allChunks = docs.flatMap((doc) => doc.chunks);
  if (allChunks.length === 0) {
    return {
      data: {
        query,
        backend: 'lexical',
        docs: docs.map((doc) => ({
          path: doc.path,
          bytes: doc.bytes,
          line_count: doc.lineCount
        })),
        result_count: 0,
        results: []
      }
    };
  }

  const queryTerms = buildQueryTerms(query);
  if (queryTerms.length === 0) {
    throw new CLIError(
      ErrorCode.VALIDATION_ERROR,
      'Query must contain at least one alphanumeric token'
    );
  }
  const queryBigrams = buildQueryBigrams(queryTerms);

  const docFrequencyByTerm = new Map<string, number>();
  for (const term of queryTerms) {
    let count = 0;
    for (const chunk of allChunks) {
      if ((chunk.termFrequency.get(term) ?? 0) > 0) {
        count += 1;
      }
    }
    docFrequencyByTerm.set(term, count);
  }

  const averageChunkLength =
    allChunks.reduce((sum, chunk) => sum + chunk.tokenCount, 0) / Math.max(allChunks.length, 1);

  const scored = allChunks
    .map((chunk) => {
      const baseScore = bm25Score({
        queryTerms,
        chunk,
        docFrequencyByTerm,
        totalChunkCount: allChunks.length,
        averageChunkLength
      });

      return {
        chunk,
        score: adjustedChunkScore({
          chunk,
          baseScore,
          queryTerms,
          queryBigrams
        })
      };
    })
    .filter((item) => item.score > 0)
    .sort((a, b) => b.score - a.score)
    .slice(0, params.topK);

  return {
    data: {
      query,
      backend: 'lexical',
      query_terms: queryTerms,
      docs: docs.map((doc) => ({
        path: doc.path,
        bytes: doc.bytes,
        line_count: doc.lineCount
      })),
      chunk_count: allChunks.length,
      result_count: scored.length,
      results: scored.map((item, idx) => ({
        rank: idx + 1,
        score: Number(item.score.toFixed(6)),
        path: item.chunk.docPath,
        accession: item.chunk.accession,
        line_start: item.chunk.lineStart,
        line_end: item.chunk.lineEnd,
        excerpt: trimExcerpt(compactWhitespace(item.chunk.text), 1200)
      }))
    }
  };
}

export async function runResearchSync(
  params: {
    id: string;
    profile: ResearchProfile;
    cacheDir?: string;
    refresh?: boolean;
  },
  context: CommandContext
): Promise<CommandResult> {
  const entity = await resolveEntity(params.id, context.secClient, { strictMapMatch: false });
  const cacheRoot = resolveCacheRoot(params.cacheDir);
  const rules = PROFILE_RULES[params.profile];

  const selectedByAccession = new Map<string, FilingRow>();

  for (const rule of rules) {
    const listResult = await runFilingsList(
      {
        id: entity.cik,
        form: rule.form,
        from: rule.recentDays ? dateDaysAgo(rule.recentDays) : undefined,
        queryLimit: rule.queryLimit
      },
      context
    );

    const rows = listResult.data as FilingRow[];
    for (const row of rows) {
      if (!selectedByAccession.has(row.accession)) {
        selectedByAccession.set(row.accession, row);
      }
    }
  }

  const selectedRows = [...selectedByAccession.values()].sort((a, b) =>
    (b.filingDate ?? '').localeCompare(a.filingDate ?? '')
  );

  const docs: CachedDoc[] = [];
  const skipped: Array<{ accession: string; reason: string }> = [];
  let fetchedCount = 0;
  let reusedCount = 0;

  for (const row of selectedRows) {
    const docPath = filingDocPath(cacheRoot, entity.cik, row.accession);
    const shouldUseCache = !params.refresh && (await fileExists(docPath));

    if (!shouldUseCache) {
      try {
        const filingResult = await runFilingsGet(
          {
            id: entity.cik,
            accession: row.accession,
            format: 'markdown'
          },
          context
        );

        const filingData = filingResult.data as { content?: unknown };
        if (typeof filingData.content !== 'string') {
          throw new CLIError(
            ErrorCode.PARSE_ERROR,
            `Unable to parse markdown content for accession ${row.accession}`
          );
        }

        await mkdir(path.dirname(docPath), { recursive: true });
        const content = filingData.content.endsWith('\n') ? filingData.content : `${filingData.content}\n`;
        await writeFile(docPath, content, 'utf8');
        fetchedCount += 1;
      } catch (error) {
        if (error instanceof CLIError && error.code === ErrorCode.NOT_FOUND) {
          skipped.push({ accession: row.accession, reason: error.message });
          continue;
        }

        throw error;
      }
    } else {
      reusedCount += 1;
    }

    docs.push({
      accession: row.accession,
      form: row.form,
      filing_date: row.filingDate,
      report_date: row.reportDate,
      filing_url: row.filingUrl,
      path: docPath
    });
  }

  const manifest: CachedManifest = {
    version: 1,
    id_input: params.id,
    cik: entity.cik,
    ticker: entity.ticker,
    title: entity.title,
    profile: params.profile,
    synced_at: nowIso(),
    docs
  };

  const { manifestPath } = await writeCachedManifest(cacheRoot, manifest);

  return {
    data: {
      id: params.id,
      cik: entity.cik,
      ticker: entity.ticker,
      title: entity.title,
      profile: params.profile,
      cache_root: cacheRoot,
      manifest_path: manifestPath,
      docs_count: docs.length,
      fetched_count: fetchedCount,
      reused_count: reusedCount,
      skipped_count: skipped.length,
      skipped,
      docs
    }
  };
}

export async function runResearchAsk(
  params: {
    query: string;
    docs: string[];
    manifestPath?: string;
    topK: number;
    chunkLines: number;
    chunkOverlap: number;
  },
  context: CommandContext
): Promise<CommandResult> {
  void context;

  const docPaths = await loadDocPaths({ docs: params.docs, manifestPath: params.manifestPath });
  if (docPaths.length === 0) {
    throw new CLIError(
      ErrorCode.DOCS_REQUIRED,
      'At least one document is required. Pass --doc <path> and/or --manifest <path>.'
    );
  }

  return runLexicalSearch({
    query: params.query,
    docPaths,
    topK: params.topK,
    chunkLines: params.chunkLines,
    chunkOverlap: params.chunkOverlap
  });
}

export async function runResearchAskById(
  params: {
    id: string;
    query: string;
    profile: ResearchProfile;
    cacheDir?: string;
    refresh?: boolean;
    topK: number;
    chunkLines: number;
    chunkOverlap: number;
  },
  context: CommandContext
): Promise<CommandResult> {
  const cacheRoot = resolveCacheRoot(params.cacheDir);
  const entity = await resolveEntity(params.id, context.secClient, { strictMapMatch: false });

  let manifest = !params.refresh
    ? await readCachedManifest(cacheRoot, entity.cik, params.profile)
    : null;

  let syncData: {
    fetched_count: number;
    reused_count: number;
    docs_count: number;
    skipped_count: number;
  } | null = null;

  if (!manifest || manifest.docs.length === 0) {
    const syncResult = await runResearchSync(
      {
        id: params.id,
        profile: params.profile,
        cacheDir: params.cacheDir,
        refresh: params.refresh
      },
      context
    );

    const syncPayload = syncResult.data as Record<string, unknown>;
    syncData = {
      fetched_count:
        typeof syncPayload.fetched_count === 'number' ? syncPayload.fetched_count : 0,
      reused_count: typeof syncPayload.reused_count === 'number' ? syncPayload.reused_count : 0,
      docs_count: typeof syncPayload.docs_count === 'number' ? syncPayload.docs_count : 0,
      skipped_count: typeof syncPayload.skipped_count === 'number' ? syncPayload.skipped_count : 0
    };

    manifest = await readCachedManifest(cacheRoot, entity.cik, params.profile);
  }

  if (!manifest || manifest.docs.length === 0) {
    throw new CLIError(
      ErrorCode.DOCS_REQUIRED,
      `No cached documents found for ${params.id} profile ${params.profile}. Run research sync first.`
    );
  }

  const docPaths = manifest.docs.map((doc) => doc.path);
  const searchResult = await runLexicalSearch({
    query: params.query,
    docPaths,
    topK: params.topK,
    chunkLines: params.chunkLines,
    chunkOverlap: params.chunkOverlap
  });
  const searchData = searchResult.data as Record<string, unknown>;

  return {
    data: {
      ...searchData,
      id: params.id,
      cik: entity.cik,
      ticker: entity.ticker,
      title: entity.title,
      profile: params.profile,
      cache_root: cacheRoot,
      manifest_path: profileManifestPath(cacheRoot, entity.cik, params.profile),
      corpus_docs_count: manifest.docs.length,
      sync: syncData ?? {
        fetched_count: 0,
        reused_count: manifest.docs.length,
        docs_count: manifest.docs.length,
        skipped_count: 0
      }
    }
  };
}
