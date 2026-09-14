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

  // The icon link in the whole garden's top bar.
  calendar(): Locator {
    return this.page.getByRole('link', { name: 'Calendar' });
  }

  // The filters sit in a details element under the title that renders closed.
  // Its summary reads Filter, followed by what is set.
  async showFilters(): Promise<void> {
    await this.page.getByText(/^Filter/).click();
  }

  care(): Locator {
    return this.page.getByLabel('Care', { exact: true });
  }

  from(): Locator {
    return this.page.getByLabel('From', { exact: true });
  }

  to(): Locator {
    return this.page.getByLabel('To', { exact: true });
  }

  async apply(): Promise<void> {
    await this.page.getByRole('button', { name: 'Apply' }).click();
  }

  clear(): Locator {
    return this.page.getByRole('link', { name: 'Clear' });
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

  // The event rows carry an id and the day markers and gaps do not. The id
  // format is the server's and lives here so a change to it is one edit.
  rows(): Locator {
    return this.page.locator('li[id^="event-"]');
  }

  // Without JavaScript the link is a navigation rather than a swap.
  async openSheet(row: Locator): Promise<void> {
    await row.getByRole('link').click();
    await this.page.waitForLoadState();
  }

  undo(row: Locator): Locator {
    return row.getByRole('button', { name: 'Undo' });
  }
}
