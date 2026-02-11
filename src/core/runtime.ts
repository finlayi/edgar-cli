import { RuntimeOptions } from './config.js';
import { SecClient } from '../sec/client.js';

export interface CommandContext {
  runtime: RuntimeOptions;
  secClient: SecClient;
}

export interface CommandResult {
  data: unknown;
  metaUpdates?: Record<string, unknown>;
}
