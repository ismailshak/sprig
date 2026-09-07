import type { Locator, Page } from '@playwright/test';

// The page a signed-in account accepts an invite on, at
// /invite/<token>/accept. The invite page links to it for somebody who
// already has an account.
export class AcceptScreen {
  constructor(private readonly page: Page) {}

  static pathOf(token: string): string {
    return `/invite/${token}/accept`;
  }

  async open(token: string): Promise<void> {
    await this.page.goto(AcceptScreen.pathOf(token));
  }

  join(garden: string): Locator {
    return this.page.getByRole('button', { name: `Join ${garden}` });
  }

  // The button on the page for an account already in the garden. It switches
  // the session to that garden.
  openGarden(garden: string): Locator {
    return this.page.getByRole('button', { name: `Open ${garden}` });
  }
}
