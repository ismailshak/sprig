import type { Locator, Page } from '@playwright/test';

// The Appearance page under More: light, dark or the device's setting.
export class AppearanceScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/appearance');
  }

  mode(label: 'Light' | 'Dark' | 'System'): Locator {
    return this.page.getByRole('radio', { name: label });
  }

  // The line under the radios, rendered for a browser running no JavaScript.
  // The page's script removes it.
  noScriptNote(): Locator {
    return this.page.getByText('Without JavaScript, sprig follows your device’s light or dark setting.');
  }
}
