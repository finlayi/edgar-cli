#!/usr/bin/env node

import { realpathSync } from 'node:fs';
import { Command, CommanderError } from 'commander';
import { fileURLToPath } from 'node:url';

import { runFactsGet } from './commands/facts.js';
import { runFilingsGet, runFilingsList } from './commands/filings.js';
import {
  parseResearchProfile,
  runResearchAsk,
  runResearchAskById,
  runResearchSync
} from './commands/research.js';
import { runResolve } from './commands/resolve.js';
import {
  buildRuntimeOptions,
  parseDateString,
  parseNonNegativeInt,
  parsePositiveInt,
  requireUserAgent
} from './core/config.js';
import { failureEnvelope, successEnvelope } from './core/envelope.js';
import { CLIError, ErrorCode, EXIT_CODE_MAP, isCLIError } from './core/errors.js';
import { shapeData } from './core/output-shape.js';
import { CommandContext, CommandResult } from './core/runtime.js';
import { SecClient } from './sec/client.js';

interface CliIo {
  stdout: (message: string) => void;
  stderr: (message: string) => void;
  env: NodeJS.ProcessEnv;
}

class CLIAbortError extends Error {
  public readonly exitCode: number;

  constructor(exitCode: number) {
    super(`CLI exited with code ${exitCode}`);
    this.exitCode = exitCode;
    this.name = 'CLIAbortError';
  }
}

function defaultIo(): CliIo {
  return {
    stdout: (message) => process.stdout.write(message),
    stderr: (message) => process.stderr.write(message),
    env: process.env
  };
}

function humanPrint(io: CliIo, data: unknown): void {
  io.stdout(`${JSON.stringify(data, null, 2)}\n`);
}

function emitSuccess(params: {
  command: string;
  result: CommandResult;
  context: CommandContext;
  io: CliIo;
}): void {
  const { context, io, command, result } = params;

  if (context.runtime.humanMode) {
    humanPrint(io, result.data);
    return;
  }

  const shaped = shapeData({
    data: result.data,
    fields: context.runtime.fields,
    limit: context.runtime.limit
  });

  const metaUpdates = {
    ...(result.metaUpdates ?? {}),
    ...shaped.metaUpdates
  };

  const envelope = successEnvelope({
    command,
    data: shaped.data,
    view: context.runtime.view,
    metaUpdates
  });

  io.stdout(`${JSON.stringify(envelope)}\n`);
}

function emitError(params: {
  command: string;
  err: CLIError;
  runtimeView: 'summary' | 'full';
  humanMode: boolean;
  io: CliIo;
}): number {
  const { command, err, runtimeView, humanMode, io } = params;

  if (humanMode) {
    io.stderr(`${err.code} ${err.message}\n`);
    return err.exitCode;
  }

  const envelope = failureEnvelope({
    command,
    code: err.code,
    message: err.message,
    retriable: err.retriable,
    view: runtimeView
  });

  io.stdout(`${JSON.stringify(envelope)}\n`);
  return err.exitCode;
}

function toCliError(err: unknown): CLIError {
  if (isCLIError(err)) {
    return err;
  }

  return new CLIError(ErrorCode.INTERNAL_ERROR, (err as Error).message || 'Unexpected error');
}

async function executeCommand(
  command: string,
  commandObj: Command,
  io: CliIo,
  handler: (context: CommandContext) => Promise<CommandResult>,
  options?: {
    requiresSecIdentity?: boolean;
  }
): Promise<void> {
  const globalOptions = commandObj.optsWithGlobals();
  const runtime = buildRuntimeOptions(
    {
      json: globalOptions.json,
      human: globalOptions.human,
      view: globalOptions.view,
      fields: globalOptions.fields,
      limit: globalOptions.limit,
      verbose: globalOptions.verbose,
      userAgent: globalOptions.userAgent
    },
    io.env
  );

  try {
    const requiresSecIdentity = options?.requiresSecIdentity ?? true;
    const userAgent = requiresSecIdentity
      ? requireUserAgent(runtime.userAgent)
      : runtime.userAgent ?? 'edgar-cli local research';
    const secClient = new SecClient({
      userAgent,
      verbose: runtime.verbose,
      logger: (message) => io.stderr(`[debug] ${message}\n`)
    });

    const context: CommandContext = {
      runtime,
      secClient
    };

    const result = await handler(context);
    emitSuccess({ command, result, context, io });
  } catch (error) {
    const cliError = toCliError(error);
    const exitCode = emitError({
      command,
      err: cliError,
      runtimeView: runtime.view,
      humanMode: runtime.humanMode,
      io
    });

    throw new CLIAbortError(exitCode);
  }
}

export function buildProgram(io: CliIo): Command {
  const program = new Command();

  program
    .name('edgar')
    .description('Agent-friendly SEC EDGAR CLI')
    .option('--json', 'Emit JSON envelope output (default)')
    .option('--human', 'Emit human-readable output')
    .option('--view <view>', 'Output view mode (summary|full)', 'summary')
    .option('--fields <fields>', 'Select specific response fields in JSON mode')
    .option('--limit <n>', 'Limit output rows in JSON mode')
    .option('--verbose', 'Enable verbose debug logs')
    .option(
      '--user-agent <value>',
      'SEC identity (required for network commands), e.g. "Name email@domain.com"'
    )
    .showHelpAfterError(true)
    .exitOverride()
    .addHelpText(
      'after',
      '\nSEC identity is required for network commands.\nSet --user-agent or EDGAR_USER_AGENT.'
    )
    .configureOutput({
      writeOut: (message) => io.stdout(message),
      writeErr: (message) => io.stderr(message)
    });

  program
    .command('resolve')
    .description('Resolve ticker/CIK to canonical SEC identity fields')
    .argument('<id>', 'Ticker (AAPL) or CIK (320193 / 0000320193)')
    .action(async function actionResolve(this: Command, id: string) {
      await executeCommand('resolve', this, io, async (context) => runResolve(id, context));
    });

  const filings = program.command('filings').description('Query filing metadata and filing documents');

  filings
    .command('list')
    .requiredOption('--id <id>', 'Ticker or CIK')
    .option('--form <form>', 'SEC form type, e.g. 10-K')
    .option('--from <yyyy-mm-dd>', 'Lower filing-date bound')
    .option('--to <yyyy-mm-dd>', 'Upper filing-date bound')
    .option('--query-limit <n>', 'Limit rows before envelope shaping')
    .option('--offset <n>', 'Offset rows before query-limit slicing', '0')
    .action(async function actionFilingsList(this: Command, options: Record<string, string>) {
      const from = options.from ? parseDateString(options.from, '--from') : undefined;
      const to = options.to ? parseDateString(options.to, '--to') : undefined;
      const queryLimit =
        options.queryLimit === undefined
          ? undefined
          : parsePositiveInt(options.queryLimit, '--query-limit');
      const offset = parseNonNegativeInt(options.offset, '--offset');

      await executeCommand('filings list', this, io, async (context) =>
        runFilingsList(
          {
            id: options.id,
            form: options.form,
            from,
            to,
            queryLimit,
            offset
          },
          context
        )
      );
    });

  filings
    .command('get')
    .requiredOption('--id <id>', 'Ticker or CIK')
    .requiredOption('--accession <accession>', 'Accession number: XXXXXXXXXX-XX-XXXXXX')
    .option('--format <format>', 'url|html|text|markdown', 'url')
    .action(async function actionFilingsGet(this: Command, options: Record<string, string>) {
      const format = options.format;
      if (!['url', 'html', 'text', 'markdown'].includes(format)) {
        throw new CLIAbortError(
          emitError({
            command: 'filings get',
            err: new CLIError(
              ErrorCode.VALIDATION_ERROR,
              '--format must be one of url|html|text|markdown'
            ),
            runtimeView: 'summary',
            humanMode: false,
            io
          })
        );
      }

      await executeCommand('filings get', this, io, async (context) =>
        runFilingsGet(
          {
            id: options.id,
            accession: options.accession,
            format: format as 'url' | 'html' | 'text' | 'markdown'
          },
          context
        )
      );
    });

  const facts = program.command('facts').description('Query SEC company facts (XBRL)');

  facts
    .command('get')
    .requiredOption('--id <id>', 'Ticker or CIK')
    .option('--taxonomy <taxonomy>', 'us-gaap|dei')
    .option('--concept <concept>', 'Concept name, e.g. Revenues')
    .option('--unit <unit>', 'Unit key, e.g. USD')
    .option('--latest', 'Return only latest point per unit')
    .action(async function actionFactsGet(this: Command, options: Record<string, string | boolean>) {
      const taxonomyValue = options.taxonomy as string | undefined;
      if (taxonomyValue && !['us-gaap', 'dei'].includes(taxonomyValue)) {
        throw new CLIAbortError(
          emitError({
            command: 'facts get',
            err: new CLIError(ErrorCode.VALIDATION_ERROR, '--taxonomy must be us-gaap or dei'),
            runtimeView: 'summary',
            humanMode: false,
            io
          })
        );
      }

      await executeCommand('facts get', this, io, async (context) =>
        runFactsGet(
          {
            id: options.id as string,
            taxonomy: taxonomyValue as 'us-gaap' | 'dei' | undefined,
            concept: options.concept as string | undefined,
            unit: options.unit as string | undefined,
            latest: Boolean(options.latest)
          },
          context
        )
      );
    });

  const research = program
    .command('research')
    .description('Run deterministic research workflows over explicit docs or cached filing profiles');

  research
    .command('sync')
    .description('Cache a deterministic research corpus for a company/profile')
    .requiredOption('--id <id>', 'Ticker or CIK')
    .option('--profile <profile>', 'core|events|financials', 'core')
    .option('--cache-dir <path>', 'Override cache directory')
    .option('--refresh', 'Force refetch even when cached docs exist')
    .action(async function actionResearchSync(
      this: Command,
      options: {
        id: string;
        profile: string;
        cacheDir?: string;
        refresh?: boolean;
      }
    ) {
      const profile = parseResearchProfile(options.profile);

      await executeCommand(
        'research sync',
        this,
        io,
        async (context) =>
          runResearchSync(
            {
              id: options.id,
              profile,
              cacheDir: options.cacheDir,
              refresh: Boolean(options.refresh)
            },
            context
          ),
        { requiresSecIdentity: true }
      );
    });

  research
    .command('ask')
    .description(
      'Query explicitly provided local docs, or a cached company profile corpus when --id is used'
    )
    .argument('<query>', 'Natural language query')
    .option('--id <id>', 'Ticker or CIK for cached/profile-based research')
    .option('--profile <profile>', 'core|events|financials (used with --id)', 'core')
    .option('--form <form>', 'SEC form filter for scoped filing selection with --id, e.g. 10-Q')
    .option('--latest <n>', 'With --id, limit to latest N filings after filters')
    .option('--cache-dir <path>', 'Override cache directory')
    .option('--refresh', 'With --id, force refetch of filings before querying')
    .option('--doc <path>', 'Path to a local document (repeatable)', collectValues, [])
    .option(
      '--manifest <path>',
      'Path to JSON manifest: either ["doc1", ...] or {"docs": ["doc1", ...]}'
    )
    .option('--top-k <n>', 'Maximum number of chunks to return', '8')
    .option('--chunk-lines <n>', 'Number of lines per retrieval chunk', '40')
    .option('--chunk-overlap <n>', 'Line overlap between retrieval chunks', '10')
    .action(async function actionResearchAsk(
      this: Command,
      query: string,
      options: {
        id?: string;
        profile: string;
        form?: string;
        latest?: string;
        cacheDir?: string;
        refresh?: boolean;
        doc: string[];
        manifest?: string;
        topK: string;
        chunkLines: string;
        chunkOverlap: string;
      }
    ) {
      const topK = parsePositiveInt(options.topK, '--top-k');
      const chunkLines = parsePositiveInt(options.chunkLines, '--chunk-lines');
      const chunkOverlap = parseNonNegativeInt(options.chunkOverlap, '--chunk-overlap');
      const latest =
        options.latest === undefined
          ? undefined
          : parsePositiveInt(options.latest, '--latest');

      if (!options.id && (options.form || latest !== undefined)) {
        throw new CLIAbortError(
          emitError({
            command: 'research ask',
            err: new CLIError(
              ErrorCode.VALIDATION_ERROR,
              '--form and --latest require --id'
            ),
            runtimeView: 'summary',
            humanMode: false,
            io
          })
        );
      }

      const requiresSecIdentity = Boolean(options.id);
      const profile = parseResearchProfile(options.profile);

      await executeCommand(
        'research ask',
        this,
        io,
        async (context) =>
          options.id
            ? runResearchAskById(
                {
                  id: options.id,
                  query,
                  profile,
                  scope: {
                    form: options.form,
                    latest
                  },
                  cacheDir: options.cacheDir,
                  refresh: Boolean(options.refresh),
                  topK,
                  chunkLines,
                  chunkOverlap
                },
                context
              )
            : runResearchAsk(
                {
                  query,
                  docs: options.doc ?? [],
                  manifestPath: options.manifest,
                  topK,
                  chunkLines,
                  chunkOverlap
                },
                context
              ),
        { requiresSecIdentity }
      );
    });

  return program;
}

function collectValues(value: string, previous: string[]): string[] {
  return [...previous, value];
}

export async function runCli(argv: string[], io: CliIo = defaultIo()): Promise<number> {
  const program = buildProgram(io);

  try {
    await program.parseAsync(argv, { from: 'user' });
    return 0;
  } catch (error) {
    if (error instanceof CLIAbortError) {
      return error.exitCode;
    }

    if (error instanceof CommanderError) {
      return error.exitCode;
    }

    const cliError = toCliError(error);
    io.stderr(`${cliError.code} ${cliError.message}\n`);
    return EXIT_CODE_MAP[cliError.code] ?? 10;
  }
}

function isDirectExecution(): boolean {
  const argvPath = process.argv[1];
  if (!argvPath) {
    return false;
  }

  try {
    return realpathSync(argvPath) === realpathSync(fileURLToPath(import.meta.url));
  } catch {
    return false;
  }
}

if (isDirectExecution()) {
  runCli(process.argv.slice(2)).then((exitCode) => {
    process.exit(exitCode);
  });
}
