import type { Locator, Page } from '@playwright/test';

export class RecoveryScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/account/recovery');
  }

  // The button reads "Create codes" for an account with none and "Create new
  // codes" for one replacing a batch.
  create(): Locator {
    return this.page.getByRole('button', { name: /^Create/ });
  }

  back(): Locator {
    return this.page.getByRole('link', { name: 'Account' });
  }

  // The codes in the box on the response that made them. The box is the only
  // list on that page.
  codes(): Locator {
    return this.page.getByRole('listitem');
  }

  // The link under the box. It goes back to Account.
  done(): Locator {
    return this.page.getByRole('link', { name: 'Done' });
  }
}
