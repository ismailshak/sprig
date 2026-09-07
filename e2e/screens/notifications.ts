import type { Locator, Page } from '@playwright/test';

const path = '/more/notifications';

export class NotificationsScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto(path);
  }

  digest(): Locator {
    return this.page.getByLabel(/What needs doing/);
  }

  activity(): Locator {
    return this.page.getByLabel(/When someone else logs care/);
  }

  // The hour is on the page only while the digest is on.
  hour(): Locator {
    return this.page.getByLabel('The digest arrives at');
  }

  save(): Locator {
    return this.page.getByRole('button', { name: 'Save changes' });
  }

  // saveChanges clicks Save changes and waits for the GET the post redirects
  // to. The click returns before the post is sent, so a test that navigates
  // straight afterwards would cancel it.
  async saveChanges(): Promise<void> {
    const reloaded = this.page.waitForResponse(
      (response) => response.request().method() === 'GET' && new URL(response.url()).pathname === path,
    );
    await this.save().click();
    await reloaded;
  }

  // The line the page's script writes the browser's refusal into.
  refusal(): Locator {
    return this.page.getByRole('alert');
  }

  // The link shown in place of the form in a browser with no push API.
  install(): Locator {
    return this.page.getByRole('link', { name: 'Install sprig' });
  }
}
