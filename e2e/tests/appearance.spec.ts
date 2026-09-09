import { people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// The choice is kept in the browser, not on the account, so the tests read it
// back from the page rather than from another sign-in.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('the dark colour scheme survives a reload and holds on the next page @js', async ({ page, more, appearance }) => {
  await more.open();
  await more.row('Appearance').click();
  await expect(page).toHaveURL('/more/appearance');
  await expect(appearance.mode('System')).toBeChecked();

  await appearance.mode('Dark').check();
  await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');

  await page.reload();
  await expect(appearance.mode('Dark')).toBeChecked();
  await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');

  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');

  await appearance.open();
  await appearance.mode('System').check();
  await page.goto('/');
  await expect(page.locator('html')).not.toHaveAttribute('data-mode');
});

test('without JavaScript the colour scheme cannot be changed @nojs', async ({ appearance }) => {
  await appearance.open();

  await expect(appearance.noScriptNote()).toBeVisible();
  await expect(appearance.mode('System')).toBeChecked();
  await expect(appearance.mode('Dark')).toBeDisabled();
});
