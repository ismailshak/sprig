import type { FullConfig } from '@playwright/test';

// compose cannot wait for the app to listen, since the distroless image has
// neither a shell nor curl for a health check to run.
export default async function globalSetup(config: FullConfig): Promise<void> {
  const baseURL = config.projects[0]?.use.baseURL;
  if (!baseURL) {
    throw new Error('no project has a baseURL');
  }
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
