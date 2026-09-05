import type { Locator, Page } from '@playwright/test';

export class ActivityScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/activity');
  }

  // Activity draws one list, with day markers, silences and events all items
  // on it.
  strand(): Locator {
    return this.page.getByRole('list');
  }

  items(): Locator {
    return this.strand().getByRole('listitem');
  }
}
