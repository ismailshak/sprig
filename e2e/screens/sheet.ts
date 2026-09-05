import type { Locator, Page } from '@playwright/test';

export class SheetScreen {
  constructor(private readonly page: Page) {}

  dialog(): Locator {
    return this.page.getByRole('dialog');
  }

  // A What chip is a button, not a radio, because clicking it fetches the sheet
  // again for that care type.
  what(care: string): Locator {
    return this.dialog().getByRole('button', { name: care, exact: true });
  }

  chip(label: string): Locator {
    return this.dialog().getByRole('radio', { name: label, exact: true });
  }

  time(): Locator {
    return this.dialog().getByLabel('Time', { exact: true });
  }

  note(): Locator {
    return this.dialog().getByLabel('Note');
  }

  // Only one primary button shows, "Log watering" or "Record a skip".
  async submit(label: string): Promise<void> {
    await this.dialog().getByRole('button', { name: label, exact: true }).click();
  }

  async cancel(): Promise<void> {
    await this.dialog().getByRole('button', { name: 'Cancel' }).click();
  }
}
