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

  // The button is disabled until the page's script enables it.
  create(): Locator {
    return this.page.getByRole('button', { name: 'Create the garden' });
  }
}
