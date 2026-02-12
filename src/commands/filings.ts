import TurndownService from 'turndown';
import { gfm } from '@joplin/turndown-plugin-gfm';

import { CLIError, ErrorCode } from '../core/errors.js';
import { CommandContext, CommandResult } from '../core/runtime.js';
import { filingDocumentUrl, submissionsUrl } from '../sec/endpoints.js';
import { dateInRange, normalizeAccession } from '../sec/normalizers.js';
import { resolveEntity } from '../sec/ticker-map.js';

interface RecentFilings {
  accessionNumber?: string[];
  filingDate?: string[];
  reportDate?: string[];
  acceptanceDateTime?: string[];
  act?: string[];
  form?: string[];
  fileNumber?: string[];
  filmNumber?: string[];
  items?: string[];
  size?: number[];
  isXBRL?: number[];
  isInlineXBRL?: number[];
  primaryDocument?: string[];
  primaryDocDescription?: string[];
}

interface SubmissionsPayload {
  cik: string;
  name?: string;
  tickers?: string[];
  filings?: {
    recent?: RecentFilings;
  };
}

interface FilingRow {
  accession: string;
  form: string | null;
  filingDate: string | null;
  reportDate: string | null;
  primaryDocument: string | null;
  filingUrl: string | null;
}

function buildMarkdownConverter(): TurndownService {
  const service = new TurndownService({
    headingStyle: 'atx',
    hr: '---',
    bulletListMarker: '-',
    codeBlockStyle: 'fenced',
    fence: '```',
    emDelimiter: '*',
    strongDelimiter: '**',
    linkStyle: 'inlined'
  });

  service.use(gfm);
  service.remove(['script', 'style', 'noscript', 'iframe', 'canvas']);

  return service;
}

const markdownConverter = buildMarkdownConverter();

function stripInlineXbrlHeaders(content: string): string {
  return content
    .replace(/<ix:header[\s\S]*?<\/ix:header>/gi, '')
    .replace(/<ix:hidden[\s\S]*?<\/ix:hidden>/gi, '')
    .replace(/<ix:resources[\s\S]*?<\/ix:resources>/gi, '');
}

function splitMarkdownTableCells(line: string): string[] {
  const trimmed = line.trim();
  const withoutLeadingPipe = trimmed.startsWith('|') ? trimmed.slice(1) : trimmed;
  const withoutTrailingPipe = withoutLeadingPipe.endsWith('|')
    ? withoutLeadingPipe.slice(0, -1)
    : withoutLeadingPipe;

  return withoutTrailingPipe.split('|').map((cell) => cell.trim());
}

function isMarkdownTableSeparatorLine(line: string): boolean {
  const cells = splitMarkdownTableCells(line);
  if (cells.length === 0) {
    return false;
  }

  return cells.every((cell) => /^:?-{3,}:?$/.test(cell.replace(/\s+/g, '')));
}

function collapseLayoutTables(markdown: string): string {
  const lines = markdown.split('\n');
  const output: string[] = [];

  for (let idx = 0; idx < lines.length; idx += 1) {
    const line = lines[idx];
    if (!line.trimStart().startsWith('|')) {
      output.push(line);
      continue;
    }

    const tableBlock = [line];
    while (idx + 1 < lines.length && lines[idx + 1].trimStart().startsWith('|')) {
      idx += 1;
      tableBlock.push(lines[idx]);
    }

    const hasSeparator = tableBlock.some(isMarkdownTableSeparatorLine);
    if (!hasSeparator) {
      output.push(...tableBlock);
      continue;
    }

    const dataRows = tableBlock.filter((row) => !isMarkdownTableSeparatorLine(row));
    const nonEmptyCellCounts = dataRows.map(
      (row) => splitMarkdownTableCells(row).filter((cell) => cell.length > 0).length
    );
    const maxNonEmptyCells = Math.max(...nonEmptyCellCounts, 0);
    const avgNonEmptyCells =
      nonEmptyCellCounts.reduce((sum, count) => sum + count, 0) /
      Math.max(nonEmptyCellCounts.length, 1);
    const isLayoutTable = maxNonEmptyCells <= 1 || avgNonEmptyCells <= 1.2;

    if (!isLayoutTable) {
      output.push(...tableBlock);
      continue;
    }

    const flattenedRows = dataRows
      .map((row) => splitMarkdownTableCells(row).filter((cell) => cell.length > 0).join(' '))
      .map((row) => row.replace(/\s+/g, ' ').trim())
      .filter((row) => row.length > 0);

    if (flattenedRows.length > 0) {
      output.push(...flattenedRows, '');
    }
  }

  return output.join('\n').replace(/\n{3,}/g, '\n\n').trim();
}

function zipRecentFilings(cik: string, recent: RecentFilings | undefined): FilingRow[] {
  if (!recent) {
    return [];
  }

  const accessionNumbers = recent.accessionNumber ?? [];
  const forms = recent.form ?? [];
  const filingDates = recent.filingDate ?? [];
  const reportDates = recent.reportDate ?? [];
  const primaryDocuments = recent.primaryDocument ?? [];

  const rowCount = accessionNumbers.length;
  const rows: FilingRow[] = [];

  for (let idx = 0; idx < rowCount; idx += 1) {
    const accession = accessionNumbers[idx];
    if (!accession) {
      continue;
    }

    const primaryDocument = primaryDocuments[idx] ?? null;
    const filingUrl =
      primaryDocument && primaryDocument.length > 0
        ? filingDocumentUrl({
            cik,
            accession,
            primaryDocument
          })
        : null;

    rows.push({
      accession,
      form: forms[idx] ?? null,
      filingDate: filingDates[idx] ?? null,
      reportDate: reportDates[idx] ?? null,
      primaryDocument,
      filingUrl
    });
  }

  return rows;
}

function extractMarkdownFromHtml(content: string): string {
  const sanitizedHtml = stripInlineXbrlHeaders(content);
  const markdown = markdownConverter
    .turndown(sanitizedHtml)
    .replace(/\u00a0/g, ' ')
    .replace(/\r/g, '')
    .replace(/[ \t]+\n/g, '\n')
    .replace(/\n[ \t]+/g, '\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();

  return collapseLayoutTables(markdown);
}

export async function runFilingsList(
  params: {
    id: string;
    form?: string;
    from?: string;
    to?: string;
    queryLimit?: number;
    offset?: number;
  },
  context: CommandContext
): Promise<CommandResult> {
  const entity = await resolveEntity(params.id, context.secClient, { strictMapMatch: false });

  const submissions = await context.secClient.fetchSecJson<SubmissionsPayload>(submissionsUrl(entity.cik));
  const rows = zipRecentFilings(entity.cik, submissions.filings?.recent);

  const normalizedForm = params.form?.toUpperCase();
  const filteredRows = rows.filter((row) => {
    if (normalizedForm && (row.form ?? '').toUpperCase() !== normalizedForm) {
      return false;
    }

    if (!row.filingDate) {
      return !params.from && !params.to;
    }

    return dateInRange(row.filingDate, params.from, params.to);
  });

  const offset = params.offset ?? 0;
  const queryLimit = params.queryLimit ?? filteredRows.length;
  const pagedRows = filteredRows.slice(offset, offset + queryLimit);

  return {
    data: pagedRows,
    metaUpdates: {
      query_total_count: filteredRows.length,
      query_returned_count: pagedRows.length,
      query_truncated: offset + pagedRows.length < filteredRows.length,
      query_offset: offset
    }
  };
}

export async function runFilingsGet(
  params: {
    id: string;
    accession: string;
    format: 'url' | 'html' | 'text' | 'markdown';
  },
  context: CommandContext
): Promise<CommandResult> {
  const accession = normalizeAccession(params.accession);
  const entity = await resolveEntity(params.id, context.secClient, { strictMapMatch: false });

  const submissions = await context.secClient.fetchSecJson<SubmissionsPayload>(submissionsUrl(entity.cik));
  const rows = zipRecentFilings(entity.cik, submissions.filings?.recent);
  const match = rows.find((row) => row.accession === accession);

  if (!match) {
    throw new CLIError(
      ErrorCode.NOT_FOUND,
      `Accession ${accession} not found in recent submissions for ${params.id}`
    );
  }

  if (!match.primaryDocument || !match.filingUrl) {
    throw new CLIError(ErrorCode.NOT_FOUND, `No primary document found for accession ${accession}`);
  }

  if (params.format === 'url') {
    return {
      data: {
        accession: match.accession,
        form: match.form,
        filingDate: match.filingDate,
        reportDate: match.reportDate,
        primaryDocument: match.primaryDocument,
        url: match.filingUrl
      }
    };
  }

  const content = await context.secClient.fetchSecText(match.filingUrl);

  if (params.format === 'html') {
    return {
      data: {
        accession: match.accession,
        url: match.filingUrl,
        content
      }
    };
  }

  return {
    data: {
      accession: match.accession,
      url: match.filingUrl,
      content: extractMarkdownFromHtml(content)
    }
  };
}
