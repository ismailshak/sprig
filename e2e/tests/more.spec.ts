import { people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test('the More tab opens the index', async ({ page, more }) => {
  await signIn(page, people.ellie.handle);

  await page.getByRole('link', { name: 'More' }).click();

  await expect(page).toHaveURL('/more');
  await expect(page.getByRole('heading', { name: 'More' })).toBeVisible();
  await expect(more.row('Account')).toBeVisible();
});

test('a member has no Garden row and no People row', async ({ page, more }) => {
  await signIn(page, people.sam.handle);

  await more.open();

  await expect(more.row('Tokens')).toBeVisible();
  await expect(more.row('Garden')).toHaveCount(0);
  await expect(more.row('People')).toHaveCount(0);
});

test('signing out ends the session', async ({ page, more }) => {
  await signIn(page, people.ellie.handle);
  await more.open();

  await more.signOut().click();

  await expect(page).toHaveURL('/dev/signin');
  await page.goto('/');
  await expect(page).toHaveURL('/dev/signin');
});
