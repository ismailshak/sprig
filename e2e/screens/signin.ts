import type { Locator, Page } from '@playwright/test';

export class SignInScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/signin');
  }

  // The button is disabled until the page's script enables it.
  signIn(): Locator {
    return this.page.getByRole('button', { name: 'Sign in with a passkey' });
  }

  // The alert above the button says why the last attempt failed. The server
  // renders it into the page when a post is refused. The page's script writes
  // it when the browser refuses before anything is posted.
  refusal(): Locator {
    return this.page.getByRole('alert');
  }
}
