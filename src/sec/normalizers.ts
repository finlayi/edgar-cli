import { z } from 'zod';

import { CLIError, ErrorCode } from '../core/errors.js';

const cikSchema = z.string().regex(/^\d{1,10}$/);
const tickerSchema = z.string().regex(/^[A-Za-z][A-Za-z0-9.-]{0,14}$/);
const accessionSchema = z.string().regex(/^\d{10}-\d{2}-\d{6}$/);

export function isLikelyCik(value: string): boolean {
  return /^\d+$/.test(value.trim());
}

export function normalizeCik(value: string): string {
  const trimmed = value.trim();
  const parsed = cikSchema.safeParse(trimmed);

  if (!parsed.success) {
    throw new CLIError(ErrorCode.VALIDATION_ERROR, `Invalid CIK: ${value}`);
  }

  return parsed.data.padStart(10, '0');
}

export function normalizeTicker(value: string): string {
  const trimmed = value.trim();
  const parsed = tickerSchema.safeParse(trimmed);

  if (!parsed.success) {
    throw new CLIError(ErrorCode.VALIDATION_ERROR, `Invalid ticker: ${value}`);
  }

  return parsed.data.toUpperCase();
}

export function normalizeAccession(value: string): string {
  const trimmed = value.trim();
  const parsed = accessionSchema.safeParse(trimmed);

  if (!parsed.success) {
    throw new CLIError(
      ErrorCode.VALIDATION_ERROR,
      '--accession must match XXXXXXXXXX-XX-XXXXXX'
    );
  }

  return parsed.data;
}

export function dateInRange(value: string, from?: string, to?: string): boolean {
  if (!from && !to) {
    return true;
  }

  if (from && value < from) {
    return false;
  }

  if (to && value > to) {
    return false;
  }

  return true;
}
