import { execFile } from 'node:child_process';
import path from 'node:path';
import { promisify } from 'node:util';

const run = promisify(execFile);

const root = path.resolve(import.meta.dirname, '../..');

// A stack is one app container and one Postgres container, started by the e2e
// task. Each worker has one of its own. A test's reseed and writes are
// invisible to the other workers.
export type Stack = { baseURL: string; databaseURL: string };

// stacks reads the stacks the e2e task started from SPRIG_BASE_URLS and
// SPRIG_DATABASE_URLS, one URL per stack in each, in the same order. It refuses
// a database the task did not create, because every test reseeds it. The task
// names its compose projects sprig-e2e-<pid>-<worker> and nothing else does.
// The allowed hostnames are the ones the seed and pgtest accept.
export function stacks(env: NodeJS.ProcessEnv): Stack[] {
  if (!env.COMPOSE_PROJECT_NAME?.startsWith('sprig-e2e-')) {
    throw new Error('refusing to run outside a stack of its own: run the suite through mise run e2e');
  }

  const baseURLs = (env.SPRIG_BASE_URLS ?? '').split(/\s+/).filter(Boolean);
  const databaseURLs = (env.SPRIG_DATABASE_URLS ?? '').split(/\s+/).filter(Boolean);
  if (baseURLs.length === 0 || baseURLs.length !== databaseURLs.length) {
    throw new Error(
      'SPRIG_BASE_URLS and SPRIG_DATABASE_URLS must list one URL per stack: run the suite through mise run e2e',
    );
  }

  for (const databaseURL of databaseURLs) {
    let hostname: string;
    try {
      hostname = new URL(databaseURL).hostname;
    } catch {
      throw new Error('SPRIG_DATABASE_URLS holds something that is not a URL');
    }

    if (!['localhost', '127.0.0.1', '[::1]', 'db'].includes(hostname)) {
      throw new Error(`refusing to run against ${hostname}: the suite only talks to loopback or the compose database`);
    }
  }

  return baseURLs.map((baseURL, i) => ({ baseURL, databaseURL: databaseURLs[i] }));
}

// seed runs the seed binary the e2e task built, named in SPRIG_SEED_BINARY,
// against one stack's database, replacing the previous run's data. It is the
// only way the suite touches a database.
export async function seed(databaseURL: string): Promise<void> {
  const binary = process.env.SPRIG_SEED_BINARY;
  if (!binary) {
    throw new Error('SPRIG_SEED_BINARY is unset: run the suite through mise run e2e');
  }
  await run(binary, [], { cwd: root, env: { ...process.env, SPRIG_DATABASE_URL: databaseURL } });
}
