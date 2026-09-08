import type { Locator, Page } from '@playwright/test';

// One photo's own page.
export class PhotoScreen {
  constructor(private readonly page: Page) {}

  picture(): Locator {
    return this.page.getByRole('img', { name: /^Photo of / });
  }

  // The line under the picture: when it was taken or uploaded, and by whom.
  meta(): Locator {
    return this.page.getByText(/^(Uploaded|Taken) /);
  }

  // Delete asks for confirmation. The first click replaces the button with
  // the question and the second confirms.
  async delete(): Promise<void> {
    await this.askToDelete();
    await this.foot().getByRole('button', { name: 'Delete' }).click();
    await this.page.waitForLoadState();
  }

  async askToDelete(): Promise<void> {
    await this.page.getByRole('button', { name: 'Delete' }).click();
    await this.page.waitForLoadState();
  }

  async cancelDelete(): Promise<void> {
    await this.foot().getByRole('button', { name: 'Cancel' }).click();
    await this.page.waitForLoadState();
  }

  // The confirmation and the button share this id. The server names it as
  // the swap target.
  foot(): Locator {
    return this.page.locator('#photo-foot');
  }
}
