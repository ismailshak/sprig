import type { Locator, Page } from '@playwright/test';

export class CloseAccountScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/account/close');
  }

  confirm(): Locator {
    return this.page.getByRole('button', { name: 'Close account' });
  }
}
