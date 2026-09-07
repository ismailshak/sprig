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
  // The timeout covers the reseed, the new page and the sign-in that run before
  // the test's first line. That setup takes about 1s. A worker's first test
  // launches the browser as well and takes about 3s. On CI four browsers and
  // eight containers share a four-core runner. The setup alone has run past 15s
  // there.
  timeout: process.env.CI ? 30_000 : 15_000,
  // Only CI retries because WebKit aborts a navigation with an internal error
  // on the Linux runners.
  retries: process.env.CI ? 2 : 0,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  use: {
    // on-first-retry records the retry. A test that fails and then passes on
    // the retry leaves a trace of the attempt that passed and none of the one
    // that failed.
    trace: 'retain-on-failure',
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
  // exists only with it off. @offline marks a test that cuts the network and
  // reads what the service worker serves. @push marks a test of the
  // Notifications form. The page only shows the form in a browser with the
  // push API, and the emulated iPhone runs as a Safari tab that has none.
  projects: [
    {
      name: 'phone',
      use: { ...devices['iPhone 16'] },
      grepInvert: /@nojs|@passkey|@offline|@push/,
    },
    {
      name: 'phone-nojs',
      use: { ...devices['iPhone 16'], javaScriptEnabled: false },
      grep: /@swap|@nojs/,
    },
    // @passkey runs on Chromium alone, because Chrome DevTools' virtual
    // authenticator is the only way to register a passkey without a real
    // device. @offline runs here too, because WebKit under Playwright fails a
    // navigation with no network even when a service worker has a response for
    // it. @push runs here because desktop Chrome has the push API. It is a
    // desktop Chrome, so this is also the only project running at a width
    // where the pages take their wide layout.
    {
      name: 'desktop',
      use: { ...devices['Desktop Chrome'] },
      grep: /@passkey|@offline|@push/,
    },
  ],
});
