import { CLIError, ErrorCode } from './errors.js';

export interface ShapedData {
  data: unknown;
  metaUpdates: Record<string, unknown>;
}

export function parseFields(raw: string | undefined): string[] | undefined {
  if (!raw) {
    return undefined;
  }

  const fields = raw
    .split(',')
    .map((field) => field.trim())
    .filter((field) => field.length > 0);

  if (fields.length === 0) {
    throw new CLIError(ErrorCode.VALIDATION_ERROR, '--fields requires at least one field');
  }

  return [...new Set(fields)];
}

function projectObject(source: Record<string, unknown>, fields: string[]): Record<string, unknown> {
  const projected: Record<string, unknown> = {};
  for (const field of fields) {
    projected[field] = source[field];
  }
  return projected;
}

function applyFields(data: unknown, fields: string[] | undefined): unknown {
  if (!fields) {
    return data;
  }

  if (Array.isArray(data)) {
    if (data.length === 0) {
      return data;
    }

    if (!data.every((item) => item && typeof item === 'object' && !Array.isArray(item))) {
      throw new CLIError(
        ErrorCode.VALIDATION_ERROR,
        '--fields can only be applied to object results or lists of objects'
      );
    }

    return data.map((item) => projectObject(item as Record<string, unknown>, fields));
  }

  if (data && typeof data === 'object' && !Array.isArray(data)) {
    return projectObject(data as Record<string, unknown>, fields);
  }

  throw new CLIError(
    ErrorCode.VALIDATION_ERROR,
    '--fields can only be applied to object results or lists of objects'
  );
}

export function shapeData(params: {
  data: unknown;
  fields?: string[];
  limit?: number;
}): ShapedData {
  const fieldShaped = applyFields(params.data, params.fields);

  const metaUpdates: Record<string, unknown> = {};

  if (Array.isArray(fieldShaped) && typeof params.limit === 'number') {
    if (params.limit < 1) {
      throw new CLIError(ErrorCode.VALIDATION_ERROR, '--limit must be at least 1');
    }

    const totalCount = fieldShaped.length;
    const data = fieldShaped.slice(0, params.limit);

    metaUpdates.total_count = totalCount;
    metaUpdates.returned_count = data.length;
    metaUpdates.truncated = data.length < totalCount;

    return {
      data,
      metaUpdates
    };
  }

  return {
    data: fieldShaped,
    metaUpdates
  };
}
