import type { Locator, Page } from '@playwright/test';

export class TokensScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/tokens');
  }

  // A token's row, found by its name. The name is the first thing in the
  // row, so the pattern is anchored to it.
  row(name: string): Locator {
    return this.page.getByRole('listitem').filter({ has: this.page.getByText(new RegExp(`^${name}$`)) });
  }

  name(): Locator {
    return this.page.getByLabel(/^Name\b/);
  }

  expiry(): Locator {
    return this.page.getByLabel(/^Expires\b/);
  }

  create(): Locator {
    return this.page.getByRole('button', { name: 'Create token' });
  }

  // The token, shown once. Every token starts with the scheme, and the prefixes
  // in the list are cut short with an ellipsis, so only the whole value matches.
  token(): Locator {
    return this.page.getByText(/^sprg_[0-9a-f]{4}_[0-9a-f]+$/);
  }

  // The button beside the token. The server renders it hidden and the page's
  // script shows it in a browser with a clipboard.
  copy(): Locator {
    return this.page.getByRole('button', { name: 'Copy' });
  }
}
