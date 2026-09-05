import type { Page } from '@playwright/test';

// signIn starts a session as the seeded user with the handle, through the
// development sign-in the app container is built with. The button's name is
// the display name followed by the handle in parentheses, and the handle is
// the part that is unique.
export async function signIn(page: Page, handle: string): Promise<void> {
  await page.goto('/dev/signin');
  await page.getByRole('button', { name: `(${handle})` }).click();
  // waitForURL holds until the post's redirect lands because click resolves
  // on the press alone. A navigation started before then interrupts the
  // redirect.
  await page.waitForURL('/');
}
