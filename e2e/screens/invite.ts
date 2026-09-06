import type { Locator, Page } from '@playwright/test';

export class InviteScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/people/invite');
  }

  // A role chip. Pressing one submits the form as a GET, so the sentence under
  // the chips changes to match with no script running.
  chip(role: string): Locator {
    return this.page.getByRole('button', { name: role, exact: true });
  }

  // The hint is inside the label, so the field's accessible name is "Until
  // optional".
  until(): Locator {
    return this.page.getByLabel(/^Until\b/);
  }

  create(): Locator {
    return this.page.getByRole('button', { name: 'Create the link' });
  }

  // The link, shown once. It is matched on the path that redeems it, because
  // no other text on the page contains that.
  link(): Locator {
    return this.page.getByText(/\/invite\//);
  }

  done(): Locator {
    return this.page.getByRole('link', { name: 'Done' });
  }

  back(): Locator {
    return this.page.getByRole('link', { name: 'People' });
  }
}
