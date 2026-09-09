import type { Locator, Page } from '@playwright/test';

export class SetupScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/setup');
  }

  garden(): Locator {
    return this.page.getByLabel('Garden name');
  }

  name(): Locator {
    return this.page.getByLabel('Display name');
  }

  // The page's script fills this in from the display name until it is typed
  // into by hand, and asks the server for a free one when the display name is
  // left. suggestHandle is the button beside it that asks again.
  handle(): Locator {
    return this.page.getByRole('textbox', { name: /^Handle/ });
  }

  suggestHandle(): Locator {
    return this.page.getByRole('button', { name: 'Suggest a handle' });
  }

  timezone(): Locator {
    return this.page.getByLabel('Timezone');
  }

  // The button on the public form is disabled until the page's script enables
  // it. The one on the signed-in page is not.
  create(): Locator {
    return this.page.getByRole('button', { name: 'Create garden' });
  }

  signInToSetUp(): Locator {
    return this.page.getByRole('link', { name: 'Sign in' });
  }
}
