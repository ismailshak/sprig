import { gardens, people, plants as seeded } from '../harness/garden';
import { serviceWorkerReady } from '../harness/service-worker';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// Every test loads Today twice: once so the worker installs, and again once it
// is active so the page goes through it and is cached. Sam is in both gardens,
// so the garden switch has somewhere to go.
test.beforeEach(async ({ page }) => {
  await signIn(page, people.sam.handle);
  await serviceWorkerReady(page);
  await page.goto('/');
});

test('a page opened before is shown offline with the time it was fetched @offline', async ({
  page,
  context,
  offlinePage,
}) => {
  await context.setOffline(true);
  await page.goto('/');

  await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();
  await expect(offlinePage.line()).toHaveText(/^You’re offline\. This page was last loaded at \d{1,2}:\d{2}/);
});

test('a page opened before today is shown offline with the day it was fetched @offline', async ({
  page,
  context,
  offlinePage,
}) => {
  // The cached copy was fetched a moment ago. A clock a day ahead makes it
  // yesterday's, so the line gives the day as well as the time.
  await page.clock.setFixedTime(new Date(Date.now() + 24 * 60 * 60 * 1000));
  await context.setOffline(true);
  await page.goto('/');

  await expect(offlinePage.line()).toHaveText(/^You’re offline\. This page was last loaded on .+ at \d{1,2}:\d{2}/);
});

test('a page never opened shows the offline page @offline', async ({ page, context, offlinePage }) => {
  await context.setOffline(true);
  await page.goto('/activity');

  await expect(offlinePage.heading()).toBeVisible();
  await expect(offlinePage.line()).toBeHidden();

  await context.setOffline(false);
  await page.getByRole('link', { name: 'Try again' }).click();
  await expect(page).toHaveURL('/activity');
  await expect(offlinePage.heading()).toHaveCount(0);
});

test('after switching garden, a page opened in the one before is not shown offline @offline', async ({
  page,
  context,
  offlinePage,
  plants,
  today,
}) => {
  await plants.open();
  await expect(plants.row(seeded.bigFella)).toBeVisible();

  await today.open();
  await today.switchGarden().click();
  await today.switchTo(gardens.upstairs.name).click();
  await expect(page.getByRole('heading', { name: gardens.upstairs.name })).toBeVisible();

  await context.setOffline(true);
  await page.goto('/plants');

  await expect(offlinePage.heading()).toBeVisible();
  await expect(page.getByText(seeded.bigFella.name)).toHaveCount(0);
});

test('after signing out, no page is shown offline @offline', async ({ page, context, offlinePage, more }) => {
  await more.open();
  await more.signOut().click();
  await expect(page).toHaveURL('/signin');

  await context.setOffline(true);
  await page.goto('/');

  await expect(offlinePage.heading()).toBeVisible();
  await expect(page.getByRole('heading', { name: gardens.home.name })).toHaveCount(0);
});
