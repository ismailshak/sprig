import type { Page } from '@playwright/test';

// serviceWorkerReady resolves once the service worker is active. A page loaded
// before that did not go through the worker and is not in its cache.
export async function serviceWorkerReady(page: Page): Promise<void> {
  await page.evaluate(() => navigator.serviceWorker.ready.then(() => undefined));
}
