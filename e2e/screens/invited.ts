import type { Locator, Page } from '@playwright/test';

// The page an invite link opens, at /invite/<token>. A token comes from the
// seed or from the link Invite someone and Re-enrol show.
export class InvitedScreen {
  constructor(private readonly page: Page) {}

  async open(token: string): Promise<void> {
    await this.page.goto(`/invite/${token}`);
  }

  // The link People and Invite someone show has no scheme: the host, then
  // /invite/, then the token. tokenOf returns the token.
  static tokenOf(link: string): string {
    const token = link.split('/invite/')[1];
    if (!token) throw new Error(`no token in the link ${link}`);
    return token.trim();
  }

  name(): Locator {
    return this.page.getByLabel('Display name');
  }

  timezone(): Locator {
    return this.page.getByLabel('Timezone');
  }

  // The submit buttons, one on the join form and one on the re-enrolment
  // form. Both are disabled until the page's script enables them.
  join(): Locator {
    return this.page.getByRole('button', { name: 'Join with a passkey' });
  }

  addDevice(): Locator {
    return this.page.getByRole('button', { name: 'Add this device' });
  }

  // The sign in link on the page for a link that cannot be used.
  signIn(): Locator {
    return this.page.getByRole('link', { name: 'sign in' });
  }
}
