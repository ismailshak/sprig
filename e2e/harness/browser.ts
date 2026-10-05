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

// sameDocument sets a property on the page's window and returns a check that
// reads it back. A reload or a navigation replaces the window, so the check
// returns false once the page has been loaded again.
export async function sameDocument(page: Page): Promise<() => Promise<boolean>> {
  await page.evaluate(() => {
    (window as Window & { sprigLoaded?: boolean }).sprigLoaded = true;
  });
  return () => page.evaluate(() => (window as Window & { sprigLoaded?: boolean }).sprigLoaded === true);
}
