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

  // saveChanges clicks Save changes and waits for the Saved line under it on
  // the page the post redirects to. Save changes is shown only without
  // JavaScript. The wait skips the hidden Saved beside each control.
  async saveChanges(): Promise<void> {
    await this.save().click();
    await this.page.getByText('Saved', { exact: true }).filter({ visible: true }).waitFor();
  }

  // The Saved beside a control. Its id is the control's id with -saved after
  // it. The server renders it hidden and the page's script shows it after a
  // save.
  savedBeside(control: 'digest' | 'activity' | 'hour'): Locator {
    return this.page.locator(`#${control}-saved`);
  }

  // The live region a swap writes its announcement into. The layout renders it
  // on every page.
  announcement(): Locator {
    return this.page.locator('#status');
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
