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

  // The two theme-color tags in the head. Their content is the colour of the
  // browser's toolbar, or of the status bar in an installed app.
  themeColors(): Locator {
    return this.page.locator('meta[name="theme-color"]');
  }

  // The line under the radios, rendered for a browser running no JavaScript.
  // The page's script removes it.
  noScriptNote(): Locator {
    return this.page.getByText('Without JavaScript, sprig follows your device’s light or dark setting.');
  }
}
