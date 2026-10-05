import { people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test('the More tab opens the index', async ({ page, more }) => {
  await signIn(page, people.ellie.handle);

  await page.getByRole('navigation').getByRole('link', { name: 'More' }).click();

  await expect(page).toHaveURL('/more');
  await expect(page.getByRole('heading', { name: 'More' })).toBeVisible();
  await expect(more.row('Account')).toBeVisible();
});

test('the More tab on a page under More goes back to the index', async ({ page, account }) => {
  await signIn(page, people.ellie.handle);
  await account.open();

  await page.getByRole('navigation').getByRole('link', { name: 'More' }).click();

  await expect(page).toHaveURL('/more');
  await expect(page.getByRole('heading', { name: 'More' })).toBeVisible();
});

test('signing out ends the session', async ({ page, more }) => {
  await signIn(page, people.ellie.handle);
  await more.open();

  await more.signOut().click();

  await expect(page).toHaveURL('/signin');
  await page.goto('/');
  await expect(page).toHaveURL('/signin');
});

// The sessionStorage key under which htmx keeps copies of Activity. Back
// restores a copy from it without a request.
const historyCache = 'htmx-history-cache';

test('signing out removes the copies of Activity kept for Back @js', async ({ page, more, activity }) => {
  await signIn(page, people.ellie.handle);
  await activity.open();
  await activity.older().click();
  await expect(page).toHaveURL(/\/activity\?before=/);
  const stored = () => page.evaluate((key) => sessionStorage.getItem(key), historyCache);
  await expect.poll(stored).not.toBeNull();

  await more.open();
  await more.signOut().click();

  await expect(page).toHaveURL('/signin');
  await expect.poll(stored).toBeNull();
});
