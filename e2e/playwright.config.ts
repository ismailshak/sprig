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
  // The tests share one database and reseed it, so they run one at a time.
  workers: 1,
  fullyParallel: false,
  retries: 0,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  use: {
    baseURL,
    trace: 'retain-on-failure',
  },
  // A test runs in both projects unless it carries the @js tag, which marks
  // behaviour that exists only with JavaScript on.
  projects: [
    {
      name: 'phone',
      use: { ...devices['Pixel 7'] },
    },
    {
      name: 'phone-nojs',
      use: { ...devices['Pixel 7'], javaScriptEnabled: false },
      grepInvert: /@js/,
    },
  ],
});
