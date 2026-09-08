import type { Locator, Page } from '@playwright/test';

export class PeopleScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/people');
  }

  // A member's row, found by the person's display name. The name is the first
  // thing in the row, so the pattern is anchored to it.
  row(name: string): Locator {
    return this.page.getByRole('listitem').filter({ has: this.page.getByText(new RegExp(`^${name}\\b`)) });
  }

  // The role select on a member's row. Its accessible name includes the
  // person's name, because the row has no visible label above it.
  role(name: string): Locator {
    return this.page.getByLabel(`${name}’s role`);
  }

  // The end date field on a member's row. Only a membership with an end date
  // has one.
  until(name: string): Locator {
    return this.page.getByLabel(`${name}’s access ends on`);
  }

  save(): Locator {
    return this.page.getByRole('button', { name: 'Save changes' });
  }

  reenrol(name: string): Locator {
    return this.page.getByRole('button', { name: `Sign-in link for ${name}` });
  }

  remove(name: string): Locator {
    return this.page.getByRole('link', { name: `Remove ${name}` });
  }

  // The two buttons under "Remove Ellie?". They replace the controls on the
  // row Remove was pressed on.
  cancelRemove(): Locator {
    return this.page.getByRole('link', { name: 'Cancel' });
  }

  confirmRemove(): Locator {
    return this.page.getByRole('button', { name: 'Remove', exact: true });
  }

  invited(): Locator {
    return this.page.getByRole('heading', { name: 'Pending invites' });
  }

  revoke(): Locator {
    return this.page.getByRole('button', { name: 'Revoke' });
  }

  inviteSomeone(): Locator {
    return this.page.getByRole('link', { name: 'Invite someone' });
  }

  // The sign-in link, shown once at the top of the page. It is matched on
  // the path that redeems it, because no other text on the page contains
  // that.
  link(): Locator {
    return this.page.getByText(/\/invite\//);
  }
}
