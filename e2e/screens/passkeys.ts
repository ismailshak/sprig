import type { Locator, Page } from '@playwright/test';

export class PasskeysScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/passkeys');
  }

  rows(): Locator {
    return this.page.getByRole('listitem');
  }

  row(device: string): Locator {
    return this.rows().filter({ hasText: device });
  }

  removeOn(device: string): Locator {
    return this.row(device).getByRole('button', { name: 'Remove' });
  }
}
