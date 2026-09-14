import { people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test('an address with no page shows Page not found and links back to Today', async ({ page, errorPage }) => {
  await signIn(page, people.sam.handle);

  const response = await page.goto('/no-such-page');

  expect(response?.status()).toBe(404);
  await expect(errorPage.heading('Page not found')).toBeVisible();
  await expect(page.getByText('There’s no page at this address.')).toBeVisible();

  await errorPage.backToToday().click();
  await expect(page).toHaveURL('/');
});
