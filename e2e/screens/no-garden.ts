import type { Locator, Page } from '@playwright/test';

// The "You're in no garden" page. Every route that needs a garden renders it
// for an account that has none. It has no URL of its own, so a test reaches it
// by signing in as such an account.
export class NoGardenScreen {
  constructor(private readonly page: Page) {}

  heading(): Locator {
    return this.page.getByRole('heading', { name: 'You’re in no garden' });
  }

  // The link to Set up your garden as this account. Rendered only when
  // sign-up is on.
  setUp(): Locator {
    return this.page.getByRole('link', { name: 'Set up a garden of your own' });
  }

  signOut(): Locator {
    return this.page.getByRole('button', { name: 'Sign out' });
  }
}
