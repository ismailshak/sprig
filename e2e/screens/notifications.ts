import type { Locator, Page } from '@playwright/test';

export class NotificationsScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/notifications');
  }

  digest(): Locator {
    return this.page.getByLabel(/What needs doing/);
  }

  activity(): Locator {
    return this.page.getByLabel(/When someone else logs care/);
  }

  // The hour is on the page only while the digest is on.
  hour(): Locator {
    return this.page.getByLabel('The digest arrives at');
  }

  save(): Locator {
    return this.page.getByRole('button', { name: 'Save changes' });
  }

  // The line the page's script writes the browser's refusal into.
  refusal(): Locator {
    return this.page.getByRole('alert');
  }

  // The link shown in place of the form in a browser with no push API.
  install(): Locator {
    return this.page.getByRole('link', { name: 'Install sprig' });
  }
}
