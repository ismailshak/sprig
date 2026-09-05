import { defineConfig, devices } from '@playwright/test';
import { requireOwnStack } from './harness/database';

const baseURL = process.env.SPRIG_BASE_URL;

if (!baseURL) {
  throw new Error('SPRIG_BASE_URL is unset: run the suite through mise run e2e');
}

requireOwnStack(process.env);

export default defineConfig({
  testDir: './tests',
  globalSetup: './harness/global-setup.ts',
  // The tests run one at a time because they share one database and reseed it.
  workers: 1,
  fullyParallel: false,
  // Only CI retries because WebKit aborts a navigation with an internal error
  // on the Linux runners.
  retries: process.env.CI ? 2 : 0,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  use: {
    baseURL,
    trace: 'retain-on-failure',
    // reducedMotion turns off the sheet's entrance animation, because the
    // stylesheet disables it under prefers-reduced-motion. Otherwise the
    // animation moves the buttons for 260ms and, with JavaScript off,
    // Playwright retries against a page timer that never fires. It goes in
    // contextOptions because there is no test option of that name.
    contextOptions: { reducedMotion: 'reduce' },
  },
  // @js marks behaviour that exists only with JavaScript on, and @nojs
  // behaviour that exists only with it off.
  projects: [
    {
      name: 'phone',
      use: { ...devices['iPhone 16'] },
      grepInvert: /@nojs/,
    },
    {
      name: 'phone-nojs',
      use: { ...devices['iPhone 16'], javaScriptEnabled: false },
      grepInvert: /@js/,
    },
  ],
});
