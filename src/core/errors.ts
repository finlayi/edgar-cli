export enum ErrorCode {
  VALIDATION_ERROR = 'VALIDATION_ERROR',
  DOCS_REQUIRED = 'DOCS_REQUIRED',
  IDENTITY_REQUIRED = 'IDENTITY_REQUIRED',
  RATE_LIMITED = 'RATE_LIMITED',
  NOT_FOUND = 'NOT_FOUND',
  NETWORK_ERROR = 'NETWORK_ERROR',
  PARSE_ERROR = 'PARSE_ERROR',
  INTERNAL_ERROR = 'INTERNAL_ERROR'
}

export const EXIT_CODE_MAP: Record<ErrorCode, number> = {
  [ErrorCode.VALIDATION_ERROR]: 2,
  [ErrorCode.DOCS_REQUIRED]: 2,
  [ErrorCode.IDENTITY_REQUIRED]: 3,
  [ErrorCode.RATE_LIMITED]: 4,
  [ErrorCode.NOT_FOUND]: 5,
  [ErrorCode.NETWORK_ERROR]: 6,
  [ErrorCode.PARSE_ERROR]: 7,
  [ErrorCode.INTERNAL_ERROR]: 10
};

export class CLIError extends Error {
  public readonly code: ErrorCode;

  public readonly retriable: boolean;

  constructor(code: ErrorCode, message: string, retriable = false) {
    super(message);
    this.code = code;
    this.retriable = retriable;
    this.name = 'CLIError';
  }

  get exitCode(): number {
    return EXIT_CODE_MAP[this.code] ?? 10;
  }
}

export function isCLIError(err: unknown): err is CLIError {
  return err instanceof CLIError;
}
