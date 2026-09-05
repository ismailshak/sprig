import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

export class TodayScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/');
  }

  section(title: 'Overdue' | 'Due today' | 'Coming up'): Locator {
    return this.page.getByRole('region', { name: title });
  }

  // The id format is the server's, and lives here so a change to it is one
  // edit.
  careRow(plant: Plant, care: string): Locator {
    return this.page.locator(`#care-${plant.id}-${care}`);
  }

  // Without JavaScript the link is a navigation rather than a swap.
  async openSheet(plant: Plant, care: string): Promise<void> {
    await this.careRow(plant, care).getByRole('link').click();
    await this.page.waitForLoadState();
  }

  careButton(plant: Plant, care: string): Locator {
    return this.careRow(plant, care).getByRole('button');
  }

  undoButton(plant: Plant, care: string): Locator {
    return this.careRow(plant, care).getByRole('button', { name: 'Undo' });
  }
}
