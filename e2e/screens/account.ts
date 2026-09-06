import type { Locator, Page } from '@playwright/test';

export class AccountScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/account');
  }

  name(): Locator {
    return this.page.getByLabel('Display name');
  }

  timezone(): Locator {
    return this.page.getByLabel('Timezone');
  }

  save(): Locator {
    return this.page.getByRole('button', { name: 'Save changes' });
  }
}
