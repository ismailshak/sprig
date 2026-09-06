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

  // add is the Add a passkey button. It is disabled until the page's script
  // runs.
  add(): Locator {
    return this.page.getByRole('button', { name: 'Add a passkey' });
  }

  // refusal is the line above Add a passkey saying why a device was not
  // enrolled. The server writes it when an answer reaches it, and the page's
  // script when the browser refuses before that.
  refusal(): Locator {
    return this.page.getByRole('alert');
  }
}
