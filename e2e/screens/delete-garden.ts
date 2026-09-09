import type { Locator, Page } from '@playwright/test';

export class DeleteGardenScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/garden/delete');
  }

  // The field the garden's name is typed into. Its label names the garden.
  name(): Locator {
    return this.page.getByLabel(/^Type .* to confirm$/);
  }

  confirm(): Locator {
    return this.page.getByRole('button', { name: 'Delete garden' });
  }
}
