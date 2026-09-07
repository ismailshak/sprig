import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

export class TodayScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/');
  }

  // The link after the garden's name that opens the garden sheet. It is
  // rendered only for an account in more than one garden.
  switchGarden(): Locator {
    return this.page.getByRole('link', { name: 'Switch garden' });
  }

  gardenSheet(): Locator {
    return this.page.getByRole('dialog', { name: 'Switch garden' });
  }

  // A garden's row in the sheet, found by its name. The name is the first
  // thing in the row, so the pattern is anchored to it.
  gardenRow(name: string): Locator {
    return this.gardenSheet()
      .getByRole('listitem')
      .filter({ has: this.page.getByText(new RegExp(`^${name}\\b`)) });
  }

  // The button that switches to a garden. The whole row is the button, so its
  // accessible name starts with the garden's name and the rest of the row
  // follows.
  switchTo(name: string): Locator {
    return this.gardenSheet().getByRole('button', { name });
  }

  section(title: 'Overdue' | 'Due today' | 'Coming up'): Locator {
    return this.page.getByRole('region', { name: title });
  }

  // The id format is the server's and lives here so a change to it is one
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

  // The server swaps the feed in under this id.
  feed(): Locator {
    return this.page.locator('#activity');
  }

  feedLines(): Locator {
    return this.feed().locator('> div');
  }

  // The stylesheet shows the feed's Undo only when JavaScript is off. With
  // JavaScript on this matches nothing.
  feedUndo(): Locator {
    return this.feed().getByRole('button', { name: 'Undo' });
  }
}
