import type { Locator, Page } from '@playwright/test';

export class AccountScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/account');
  }

  // The value shown for label while the form is closed. The page lists each
  // label as a term with its value as the definition after it.
  shown(label: string): Locator {
    return this.page.getByRole('term').filter({ hasText: label }).locator('xpath=following-sibling::dd[1]');
  }

  edit(): Locator {
    return this.page.getByRole('link', { name: 'Edit', exact: true });
  }

  name(): Locator {
    return this.page.getByLabel('Display name');
  }

  handle(): Locator {
    return this.page.getByLabel('Handle');
  }

  recoveryCodes(): Locator {
    return this.page.getByRole('link', { name: /^Recovery codes/ });
  }

  timezone(): Locator {
    return this.page.getByLabel('Timezone');
  }

  save(): Locator {
    return this.page.getByRole('button', { name: 'Save changes' });
  }

  cancel(): Locator {
    return this.page.getByRole('link', { name: 'Cancel' });
  }

  closeAccount(): Locator {
    return this.page.getByRole('link', { name: 'Close account' });
  }
}
