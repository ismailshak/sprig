import type { Locator, Page } from '@playwright/test';

export class CloseAccountScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/account/close');
  }

  // The field the account's handle is typed into. Its label shows the handle.
  handle(): Locator {
    return this.page.getByLabel(/^Type .* to confirm$/);
  }

  confirm(): Locator {
    return this.page.getByRole('button', { name: 'Close account' });
  }
}
