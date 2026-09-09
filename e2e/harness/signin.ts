import type { Page } from '@playwright/test';

// signIn signs in as the seeded user with this handle, through the development
// sign-in page the app container is built with. The button is matched on the
// handle in parentheses, because display names are not unique.
export async function signIn(page: Page, handle: string): Promise<void> {
  await page.goto('/dev/signin');
  await page.getByRole('button', { name: `(${handle})` }).click();
  // click resolves as soon as the button is pressed, so waitForURL waits for
  // the redirect to finish. A navigation started before that would interrupt
  // it.
  await page.waitForURL('/');
  // networkidle waits for the heading font. The page preloads it, and with
  // JavaScript off the page can reach load before that request finishes. A
  // navigation started while the font is still loading fails with an internal
  // WebKit error.
  await page.waitForLoadState('networkidle');
}
