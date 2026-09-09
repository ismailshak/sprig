import type { Locator, Page } from '@playwright/test';

// The Reminders page, the last step of setting up a garden or joining one.
export class RemindersScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/setup/reminders');
  }

  // Turn on notifications is a link. With JavaScript the press subscribes this
  // browser and opens Today. Without it the link opens the Notifications page.
  turnOn(): Locator {
    return this.page.getByRole('link', { name: 'Turn on notifications' });
  }

  notNow(): Locator {
    return this.page.getByRole('link', { name: 'Not now' });
  }

  // The line the page's script writes the browser's refusal into.
  refusal(): Locator {
    return this.page.getByRole('alert');
  }

  // The install steps the script shows in place of the offer in a browser
  // with no push API, and the Continue link under them.
  steps(): Locator {
    return this.page.getByRole('list').getByRole('listitem');
  }

  continueLink(): Locator {
    return this.page.getByRole('link', { name: 'Continue' });
  }
}
