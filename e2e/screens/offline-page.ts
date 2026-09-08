import type { Locator, Page } from '@playwright/test';

// The two things the service worker shows when the network is gone: a cached
// page with a line at the top saying when it was fetched, or the offline page.
export class OfflinePageScreen {
  constructor(private readonly page: Page) {}

  line(): Locator {
    return this.page.getByRole('status');
  }

  heading(): Locator {
    return this.page.getByRole('heading', { name: 'You’re offline' });
  }
}
