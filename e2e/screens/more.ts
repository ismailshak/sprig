import type { Locator, Page } from '@playwright/test';

export class MoreScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more');
  }

  // A row is a link whose text starts with its label. A row with something to
  // report puts that after the label, inside the same link.
  row(label: string): Locator {
    return this.page.getByRole('link', { name: new RegExp(`^${label}\\b`) });
  }

  signOut(): Locator {
    return this.page.getByRole('button', { name: 'Sign out' });
  }

  install(): Locator {
    return this.page.getByRole('link', { name: 'Install sprig' });
  }
}
