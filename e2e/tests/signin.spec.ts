import { expect, test } from '../harness/test';

test('a signed-out visitor is redirected to sign in', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveURL('/dev/signin');
  await expect(page.getByRole('heading', { name: 'Development sign-in' })).toBeVisible();
});

test('a session survives a reload', async ({ page }) => {
  await page.goto('/dev/signin');
  await page.getByRole('button', { name: 'Ellie (ellie)' }).click();
  await expect(page).toHaveURL('/');

  await page.reload();
  await expect(page).toHaveURL('/');
});
