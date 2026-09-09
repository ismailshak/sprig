import type { Locator, Page } from '@playwright/test';

export class ArchivedPlantsScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/plants/archived');
  }

  rows(): Locator {
    return this.page.getByRole('listitem');
  }

  // The pattern is anchored at the start because a plant's name can appear as
  // another plant's second line.
  row(name: string): Locator {
    return this.page.getByRole('link', { name: new RegExp(`^${name}(\\s|$)`) });
  }
}
