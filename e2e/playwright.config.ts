import { defineConfig, devices } from '@playwright/test';
import { stacks } from './harness/database';

// The e2e task starts one app and Postgres stack per worker and decides how
// many. The suite runs exactly that many workers.
const workers = stacks(process.env).length;

export default defineConfig({
  testDir: './tests',
  globalSetup: './harness/global-setup.ts',
  workers,
  fullyParallel: true,
  // The slowest test takes 6s. A test still running at 15s is stuck.
  timeout: 15_000,
  // Only CI retries because WebKit aborts a navigation with an internal error
  // on the Linux runners.
  retries: process.env.CI ? 2 : 0,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  use: {
    trace: process.env.CI ? 'on-first-retry' : 'retain-on-failure',
    // reducedMotion turns off the sheet's entrance animation, because the
    // stylesheet disables it under prefers-reduced-motion. Otherwise the
    // animation moves the buttons for 260ms and, with JavaScript off,
    // Playwright retries against a page timer that never fires. It goes in
    // contextOptions because there is no test option of that name.
    contextOptions: { reducedMotion: 'reduce' },
  },
  // @swap marks a test whose action is a form submission that JavaScript turns
  // into an htmx swap. Without JavaScript the same submission is a post and a
  // redirect, and the no-JavaScript project runs only those tests. @js marks
  // behaviour that exists only with JavaScript on, and @nojs behaviour that
  // exists only with it off.
  projects: [
    {
      name: 'phone',
      use: { ...devices['iPhone 16'] },
      grepInvert: /@nojs/,
    },
    {
      name: 'phone-nojs',
      use: { ...devices['iPhone 16'], javaScriptEnabled: false },
      grep: /@swap|@nojs/,
    },
  ],
});
