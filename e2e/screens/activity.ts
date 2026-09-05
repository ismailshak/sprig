import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

export class ActivityScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/activity');
  }

  // The log filtered to one plant has a link back to that plant at the top.
  // The whole garden's log has no such link.
  backTo(plant: Plant): Locator {
    return this.page.getByRole('link', { name: plant.name });
  }

  older(): Locator {
    return this.page.getByRole('link', { name: 'Older activity' });
  }

  latest(): Locator {
    return this.page.getByRole('link', { name: 'Latest activity' });
  }

  // Activity renders one list. Day markers, gaps and events are all items in
  // it.
  list(): Locator {
    return this.page.getByRole('list');
  }

  items(): Locator {
    return this.list().getByRole('listitem');
  }
}
