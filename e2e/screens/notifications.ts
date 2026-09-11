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

  // saveChanges clicks Save changes and waits for the Saved line. With
  // JavaScript the save is a swap and without it a post and a redirect. The
  // line is on the page once either has finished.
  async saveChanges(): Promise<void> {
    await this.save().click();
    await this.page.getByText('Saved', { exact: true }).waitFor();
  }

  // The button under Subscribed devices that subscribes this browser. The
  // server renders it hidden and the page's script shows it.
  addDevice(): Locator {
    return this.page.getByRole('button', { name: 'Add this device' });
  }

  // The line the page's script writes into when Add this device is refused.
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
}
