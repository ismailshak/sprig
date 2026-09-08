import type { Locator, Page } from '@playwright/test';

// The Recover an account page, at /recover. It is public. Nothing on it signs
// anybody in: a code lets the browser register a passkey, and the person signs
// in with that afterwards.
export class RecoverScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/recover');
  }

  code(): Locator {
    return this.page.getByLabel('Recovery code');
  }

  use(): Locator {
    return this.page.getByRole('button', { name: 'Continue' });
  }

  // The button on the form a matched code opens. It is disabled until the
  // page's script enables it.
  registerPasskey(): Locator {
    return this.page.getByRole('button', { name: 'Add passkey' });
  }

  // The link under the sentence on a page that turned the person away. It
  // goes back to the form.
  tryAnother(): Locator {
    return this.page.getByRole('link', { name: 'Try another code' });
  }
}
