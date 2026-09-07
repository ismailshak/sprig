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

  timezone(): Locator {
    return this.page.getByLabel('Timezone');
  }

  // The button on the public form is disabled until the page's script enables
  // it. The one on the signed-in page is not.
  create(): Locator {
    return this.page.getByRole('button', { name: 'Create the garden' });
  }

  signInToSetUp(): Locator {
    return this.page.getByRole('link', { name: 'Sign in to set up a garden as yourself' });
  }
}
