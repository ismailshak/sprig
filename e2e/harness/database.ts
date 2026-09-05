import { execFile } from 'node:child_process';
import path from 'node:path';
import { promisify } from 'node:util';

const run = promisify(execFile);

const root = path.resolve(import.meta.dirname, '../..');

// requireOwnStack refuses to run against a database the e2e task did not
// create, because every test reseeds it. The task names its compose project
// sprig-e2e-<pid> and nothing else does. The allowed hostnames are the ones the
// seed and pgtest accept.
export function requireOwnStack(env: NodeJS.ProcessEnv): void {
  if (!env.COMPOSE_PROJECT_NAME?.startsWith('sprig-e2e-')) {
    throw new Error('refusing to run outside a stack of its own: run the suite through mise run e2e');
  }

  if (!env.SPRIG_DATABASE_URL) {
    throw new Error('SPRIG_DATABASE_URL is unset: run the suite through mise run e2e');
  }

  let hostname: string;
  try {
    hostname = new URL(env.SPRIG_DATABASE_URL).hostname;
  } catch {
    throw new Error('SPRIG_DATABASE_URL is not a URL');
  }

  if (!['localhost', '127.0.0.1', '[::1]', 'db'].includes(hostname)) {
    throw new Error(`refusing to run against ${hostname}: the suite only talks to loopback or the compose database`);
  }
}

// seed runs the Go seed against SPRIG_DATABASE_URL, replacing the previous
// run's data. It is the only way the suite touches the database.
export async function seed(): Promise<void> {
  await run('go', ['run', './cmd/seed'], { cwd: root });
}
