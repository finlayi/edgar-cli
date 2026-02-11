import * as cheerio from 'cheerio';

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

function extractTextFromHtml(content: string): string {
  const $ = cheerio.load(content);
  return $.text().replace(/\s+/g, ' ').trim();
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
    format: 'url' | 'html' | 'text';
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
      content: extractTextFromHtml(content)
    }
  };
}
