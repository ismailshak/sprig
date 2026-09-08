import type { Locator, Page } from '@playwright/test';

export class InstallScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/install');
  }

  // The platform is chosen rather than detected, because this URL is often
  // read on a device other than the one being set up.
  platform(label: string): Locator {
    return this.page.getByRole('button', { name: label });
  }

  steps(): Locator {
    return this.page.getByRole('list').getByRole('listitem');
  }

  // The Continue link at the end of /install?after=invite. It opens Today.
  // The page reached from More does not have it.
  continueLink(): Locator {
    return this.page.getByRole('link', { name: 'Continue' });
  }
}
