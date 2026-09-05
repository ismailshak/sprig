import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

export class ActivityScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/activity');
  }

  // The log filtered to one plant is headed by a link back to that plant, and
  // the whole garden's log has no link of its own.
  backTo(plant: Plant): Locator {
    return this.page.getByRole('link', { name: plant.name });
  }

  older(): Locator {
    return this.page.getByRole('link', { name: 'Older activity' });
  }

  latest(): Locator {
    return this.page.getByRole('link', { name: 'Latest activity' });
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
