import { normalizeAccession, normalizeCik } from './normalizers.js';

export const SEC_DATA_HOST = 'https://data.sec.gov';
export const SEC_WWW_HOST = 'https://www.sec.gov';

export function submissionsUrl(cik: string): string {
  const cik10 = normalizeCik(cik);
  return `${SEC_DATA_HOST}/submissions/CIK${cik10}.json`;
}

export function companyFactsUrl(cik: string): string {
  const cik10 = normalizeCik(cik);
  return `${SEC_DATA_HOST}/api/xbrl/companyfacts/CIK${cik10}.json`;
}

export function tickerMapUrl(): string {
  return `${SEC_WWW_HOST}/files/company_tickers.json`;
}

export function filingDocumentUrl(params: {
  cik: string;
  accession: string;
  primaryDocument: string;
}): string {
  const cikNumeric = String(Number.parseInt(normalizeCik(params.cik), 10));
  const accessionNoDash = normalizeAccession(params.accession).replace(/-/g, '');
  return `${SEC_WWW_HOST}/Archives/edgar/data/${cikNumeric}/${accessionNoDash}/${params.primaryDocument}`;
}
