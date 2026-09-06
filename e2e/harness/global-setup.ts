import { stacks } from './database';

// Waits for every stack's app to listen. compose cannot wait for the app
// itself, because the distroless image has no shell or curl for a health check.
export default async function globalSetup(): Promise<void> {
  await Promise.all(stacks(process.env).map((stack) => waitForApp(stack.baseURL)));
}

async function waitForApp(baseURL: string): Promise<void> {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(new URL('/healthz', baseURL));
      if (response.ok) {
        return;
      }
    } catch {
      // Not listening yet.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`the app at ${baseURL} did not answer /healthz within 30s`);
}
