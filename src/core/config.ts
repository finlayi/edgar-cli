import { z } from 'zod';

import { CLIError, ErrorCode } from './errors.js';
import { parseFields } from './output-shape.js';

const positiveIntSchema = z.coerce.number().int().min(1);
const nonNegativeIntSchema = z.coerce.number().int().min(0);
const dateSchema = z.string().regex(/^\d{4}-\d{2}-\d{2}$/);

export interface RuntimeOptions {
  jsonMode: boolean;
  humanMode: boolean;
  view: 'summary' | 'full';
  fields?: string[];
  limit?: number;
  verbose: boolean;
  userAgent?: string;
}

export interface RuntimeInput {
  json?: boolean;
  human?: boolean;
  view?: string;
  fields?: string;
  limit?: string | number;
  verbose?: boolean;
  userAgent?: string;
}

export function buildRuntimeOptions(input: RuntimeInput, env: NodeJS.ProcessEnv): RuntimeOptions {
  const humanMode = Boolean(input.human);
  const jsonMode = !humanMode;

  const view = input.view === 'full' ? 'full' : 'summary';

  const parsedFields = parseFields(input.fields);
  const parsedLimit =
    input.limit === undefined || input.limit === null
      ? undefined
      : parsePositiveInt(String(input.limit), '--limit');

  return {
    jsonMode,
    humanMode,
    view,
    fields: parsedFields,
    limit: parsedLimit,
    verbose: Boolean(input.verbose),
    userAgent: input.userAgent?.trim() || env.EDGAR_USER_AGENT?.trim() || undefined
  };
}

export function requireUserAgent(userAgent: string | undefined): string {
  if (userAgent && userAgent.trim().length > 0) {
    return userAgent.trim();
  }

  throw new CLIError(
    ErrorCode.IDENTITY_REQUIRED,
    'Missing SEC identity. Set --user-agent "Name email@domain.com" or EDGAR_USER_AGENT.'
  );
}

export function parsePositiveInt(value: string, argName: string): number {
  const parsed = positiveIntSchema.safeParse(value);
  if (!parsed.success) {
    throw new CLIError(ErrorCode.VALIDATION_ERROR, `${argName} must be a positive integer`);
  }
  return parsed.data;
}

export function parseNonNegativeInt(value: string, argName: string): number {
  const parsed = nonNegativeIntSchema.safeParse(value);
  if (!parsed.success) {
    throw new CLIError(ErrorCode.VALIDATION_ERROR, `${argName} must be a non-negative integer`);
  }
  return parsed.data;
}

export function parseDateString(value: string, argName: string): string {
  const parsed = dateSchema.safeParse(value);
  if (!parsed.success) {
    throw new CLIError(ErrorCode.VALIDATION_ERROR, `${argName} must use YYYY-MM-DD`);
  }
  return parsed.data;
}
