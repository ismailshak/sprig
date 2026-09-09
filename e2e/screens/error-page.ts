import type { Locator, Page } from '@playwright/test';

// The error page: Page not found, Something went wrong, or This request
// couldn't be processed. It has no URL of its own. A test reaches it by asking
// for an address that has no page.
export class ErrorPageScreen {
  constructor(private readonly page: Page) {}

  heading(name: string): Locator {
    return this.page.getByRole('heading', { name });
  }

  backToToday(): Locator {
    return this.page.getByRole('link', { name: 'Back to Today' });
  }
}
