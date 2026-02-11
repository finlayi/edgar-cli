#!/usr/bin/env node

import { Command, CommanderError } from 'commander';
import { pathToFileURL } from 'node:url';

import { runFactsGet } from './commands/facts.js';
import { runFilingsGet, runFilingsList } from './commands/filings.js';
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
  handler: (context: CommandContext) => Promise<CommandResult>
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
    const userAgent = requireUserAgent(runtime.userAgent);
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
    .option('--format <format>', 'url|html|text', 'url')
    .action(async function actionFilingsGet(this: Command, options: Record<string, string>) {
      const format = options.format;
      if (!['url', 'html', 'text'].includes(format)) {
        throw new CLIAbortError(
          emitError({
            command: 'filings get',
            err: new CLIError(ErrorCode.VALIDATION_ERROR, '--format must be one of url|html|text'),
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
            format: format as 'url' | 'html' | 'text'
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

  return program;
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

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  runCli(process.argv.slice(2)).then((exitCode) => {
    process.exit(exitCode);
  });
}
