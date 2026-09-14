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
