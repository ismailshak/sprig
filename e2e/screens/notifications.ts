import type { Locator, Page } from '@playwright/test';

const path = '/more/notifications';

export class NotificationsScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto(path);
  }

  digest(): Locator {
    return this.page.getByLabel(/Daily digest/);
  }

  activity(): Locator {
    return this.page.getByLabel(/When someone else logs care/);
  }

  // The hour is on the page only while the digest is on.
  hour(): Locator {
    return this.page.getByLabel('Send the digest at');
  }

  save(): Locator {
    return this.page.getByRole('button', { name: 'Save changes' });
  }

  // saveChanges clicks Save changes and waits for the page the post redirects
  // to to finish loading. The click returns before the post is sent, and the
  // redirect is still navigating when its response arrives, so a test that
  // opens a page after either would cancel the navigation under way.
  async saveChanges(): Promise<void> {
    const loaded = this.page.waitForEvent('load');
    await this.save().click();
    await loaded;
  }

  // The line the page's script writes the browser's refusal into.
  refusal(): Locator {
    return this.page.getByRole('alert');
  }

  // The link shown in place of the form in a browser with no push API.
  install(): Locator {
    return this.page.getByRole('link', { name: 'Install sprig' });
  }

  sendTest(): Locator {
    return this.page.getByRole('button', { name: 'Send test notification' });
  }

  // The line under the Send test notification button saying how the test went.
  testResult(): Locator {
    return this.page.getByRole('status');
  }
}
