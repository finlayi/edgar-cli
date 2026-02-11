import pLimit from 'p-limit';

import { CLIError, ErrorCode } from '../core/errors.js';

const REQUEST_INTERVAL_MS = 125;
const MAX_ATTEMPTS = 4;
const paceGate = pLimit(1);
let nextAllowedAt = 0;

const sleep = async (ms: number): Promise<void> => {
  if (ms <= 0) {
    return;
  }

  await new Promise((resolve) => setTimeout(resolve, ms));
};

const jitter = (): number => Math.floor(Math.random() * 120);

function retryAfterMs(headerValue: string | null): number | null {
  if (!headerValue) {
    return null;
  }

  const seconds = Number.parseInt(headerValue, 10);
  if (!Number.isNaN(seconds)) {
    return Math.max(0, seconds * 1000);
  }

  const retryAt = Date.parse(headerValue);
  if (Number.isNaN(retryAt)) {
    return null;
  }

  return Math.max(0, retryAt - Date.now());
}

async function paceRequests(): Promise<void> {
  await paceGate(async () => {
    const now = Date.now();
    const delay = Math.max(0, nextAllowedAt - now);
    if (delay > 0) {
      await sleep(delay);
    }

    nextAllowedAt = Math.max(nextAllowedAt, Date.now()) + REQUEST_INTERVAL_MS;
  });
}

function isUndeclaredAutomationBody(value: string): boolean {
  const lowered = value.toLowerCase();
  return (
    lowered.includes('undeclared automated tool') ||
    lowered.includes('please declare your traffic') ||
    lowered.includes('acceptable policy')
  );
}

function isRetriableNetworkError(err: unknown): boolean {
  return err instanceof TypeError;
}

function toNetworkError(url: string, message: string, retriable = false): CLIError {
  return new CLIError(ErrorCode.NETWORK_ERROR, `SEC request failed for ${url}: ${message}`, retriable);
}

function toRateLimitedError(url: string): CLIError {
  return new CLIError(ErrorCode.RATE_LIMITED, `SEC rate limit reached for ${url}`, true);
}

export interface SecClientOptions {
  userAgent: string;
  verbose?: boolean;
  fetchImpl?: typeof fetch;
  logger?: (message: string) => void;
}

export class SecClient {
  private readonly userAgent: string;

  private readonly verbose: boolean;

  private readonly fetchImpl: typeof fetch;

  private readonly logger: (message: string) => void;

  constructor(options: SecClientOptions) {
    this.userAgent = options.userAgent;
    this.verbose = options.verbose ?? false;
    this.fetchImpl = options.fetchImpl ?? fetch;
    this.logger = options.logger ?? (() => undefined);
  }

  async fetchSecJson<T>(url: string): Promise<T> {
    const raw = await this.request(url, 'json');

    try {
      return JSON.parse(raw) as T;
    } catch (error) {
      throw new CLIError(
        ErrorCode.PARSE_ERROR,
        `Unable to parse SEC JSON response from ${url}: ${(error as Error).message}`
      );
    }
  }

  async fetchSecText(url: string): Promise<string> {
    return this.request(url, 'text');
  }

  private log(message: string): void {
    if (!this.verbose) {
      return;
    }

    this.logger(message);
  }

  private async request(url: string, kind: 'json' | 'text'): Promise<string> {
    for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt += 1) {
      try {
        await paceRequests();
        this.log(`GET ${url} (attempt ${attempt}/${MAX_ATTEMPTS})`);

        const response = await this.fetchImpl(url, {
          method: 'GET',
          headers: {
            'User-Agent': this.userAgent,
            'Accept-Encoding': 'identity'
          }
        });

        if (response.status === 403) {
          const body = await response.text();
          if (isUndeclaredAutomationBody(body)) {
            throw new CLIError(
              ErrorCode.IDENTITY_REQUIRED,
              'SEC rejected request as undeclared automation. Use a valid --user-agent or EDGAR_USER_AGENT.'
            );
          }

          throw toNetworkError(url, '403 Forbidden');
        }

        if (response.status === 404) {
          throw new CLIError(ErrorCode.NOT_FOUND, `SEC resource not found at ${url}`);
        }

        if (response.status === 429) {
          if (attempt < MAX_ATTEMPTS) {
            const headerDelay = retryAfterMs(response.headers.get('retry-after'));
            const delay = headerDelay ?? 250 * 2 ** (attempt - 1) + jitter();
            this.log(`429 received, waiting ${delay}ms before retry`);
            await sleep(delay);
            continue;
          }
          throw toRateLimitedError(url);
        }

        if (response.status === 503) {
          if (attempt < MAX_ATTEMPTS) {
            const headerDelay = retryAfterMs(response.headers.get('retry-after'));
            const delay = headerDelay ?? 250 * 2 ** (attempt - 1) + jitter();
            this.log(`503 received, waiting ${delay}ms before retry`);
            await sleep(delay);
            continue;
          }

          throw toNetworkError(url, '503 Service Unavailable', true);
        }

        if (!response.ok) {
          if (response.status >= 500 && attempt < MAX_ATTEMPTS) {
            const delay = 250 * 2 ** (attempt - 1) + jitter();
            this.log(`HTTP ${response.status}, waiting ${delay}ms before retry`);
            await sleep(delay);
            continue;
          }

          throw toNetworkError(url, `HTTP ${response.status}`);
        }

        const body = await response.text();
        if (kind === 'json' && body.trim().length === 0) {
          throw new CLIError(ErrorCode.PARSE_ERROR, `SEC returned empty JSON response from ${url}`);
        }

        return body;
      } catch (error) {
        if (error instanceof CLIError) {
          throw error;
        }

        if (attempt < MAX_ATTEMPTS && isRetriableNetworkError(error)) {
          const delay = 250 * 2 ** (attempt - 1) + jitter();
          this.log(`Transient network error, waiting ${delay}ms before retry`);
          await sleep(delay);
          continue;
        }

        throw toNetworkError(url, (error as Error).message, isRetriableNetworkError(error));
      }
    }

    throw toNetworkError(url, 'Request failed after retries');
  }
}
