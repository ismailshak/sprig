import type { Locator, Page } from '@playwright/test';

export class SheetScreen {
  constructor(private readonly page: Page) {}

  dialog(): Locator {
    return this.page.getByRole('dialog');
  }

  // Every chip on the sheet is a radio: the care type, the outcome, when it
  // happened and the reminder.
  chip(label: string): Locator {
    return this.dialog().getByRole('radio', { name: label, exact: true });
  }

  time(): Locator {
    return this.dialog().getByLabel('Time', { exact: true });
  }

  note(): Locator {
    return this.dialog().getByLabel('Note');
  }

  // Only one primary button shows, "Log watering" or "Log skip".
  async submit(label: string): Promise<void> {
    await this.dialog().getByRole('button', { name: label, exact: true }).click();
  }

  // Opened over an event already logged, the sheet saves rather than logs and
  // Delete is beside the primary button.
  deleteButton(): Locator {
    return this.dialog().getByRole('button', { name: 'Delete' });
  }

  loggedBy(): Locator {
    return this.dialog().getByText(/^Logged by /);
  }

  async cancel(): Promise<void> {
    await this.dialog().getByRole('button', { name: 'Cancel' }).click();
  }
}
