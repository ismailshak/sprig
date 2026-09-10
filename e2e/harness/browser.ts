import type { Browser, BrowserContext, Page } from '@playwright/test';

// asAnotherBrowser opens a second browser context on the same app. It carries
// no cookies from the first, so one test can have two people signed in at
// once. The caller closes the context. Otherwise the browser fixture holds it
// open for the rest of the worker.
export async function asAnotherBrowser(
  browser: Browser,
  baseURL: string | undefined,
): Promise<{ page: Page; context: BrowserContext }> {
  const context = await browser.newContext({ baseURL });
  return { page: await context.newPage(), context };
}
