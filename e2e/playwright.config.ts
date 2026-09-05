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
  // Only CI retries because WebKit aborts a navigation with an internal error
  // on the Linux runners.
  retries: process.env.CI ? 2 : 0,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  use: {
    baseURL,
    trace: 'retain-on-failure',
    // The sheet's entrance animation moves its buttons for 260ms, and
    // Playwright's retry after an unstable check sleeps on a page timer that
    // never fires with JavaScript off. The stylesheet stops the animation
    // under prefers-reduced-motion. reducedMotion sits in contextOptions
    // because the runner takes no test option of that name.
    contextOptions: { reducedMotion: 'reduce' },
  },
  // A test runs in both projects unless it carries the @js tag, which marks
  // behaviour that exists only with JavaScript on.
  projects: [
    {
      name: 'phone',
      use: { ...devices['iPhone 16'] },
    },
    {
      name: 'phone-nojs',
      use: { ...devices['iPhone 16'], javaScriptEnabled: false },
      grepInvert: /@js/,
    },
  ],
});
