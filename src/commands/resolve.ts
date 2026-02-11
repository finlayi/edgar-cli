import { CommandContext, CommandResult } from '../core/runtime.js';
import { resolveEntity } from '../sec/ticker-map.js';

export async function runResolve(id: string, context: CommandContext): Promise<CommandResult> {
  const entity = await resolveEntity(id, context.secClient, { strictMapMatch: true });

  return {
    data: entity
  };
}
