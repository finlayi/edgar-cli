import { ErrorCode } from './errors.js';

export interface BrokerError {
  code: ErrorCode;
  message: string;
  retriable: boolean;
}

export interface OutputEnvelope {
  ok: boolean;
  command: string;
  provider: 'sec';
  data: unknown;
  error: BrokerError | null;
  meta: Record<string, unknown>;
}

function nowIso(): string {
  return new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
}

export function successEnvelope(params: {
  command: string;
  data: unknown;
  view: string;
  metaUpdates?: Record<string, unknown>;
}): OutputEnvelope {
  return {
    ok: true,
    command: params.command,
    provider: 'sec',
    data: params.data,
    error: null,
    meta: {
      timestamp: nowIso(),
      output_schema: 'v1',
      view: params.view,
      ...(params.metaUpdates ?? {})
    }
  };
}

export function failureEnvelope(params: {
  command: string;
  code: ErrorCode;
  message: string;
  retriable?: boolean;
  view: string;
  metaUpdates?: Record<string, unknown>;
}): OutputEnvelope {
  return {
    ok: false,
    command: params.command,
    provider: 'sec',
    data: null,
    error: {
      code: params.code,
      message: params.message,
      retriable: params.retriable ?? false
    },
    meta: {
      timestamp: nowIso(),
      output_schema: 'v1',
      view: params.view,
      ...(params.metaUpdates ?? {})
    }
  };
}
