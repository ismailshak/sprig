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

test('the theme colour follows a change of colour scheme without a reload @js', async ({ page, appearance }) => {
  const contentAndMedia = () =>
    appearance
      .themeColors()
      .evaluateAll((tags) => tags.map((tag) => [tag.getAttribute('content'), tag.getAttribute('media')]));
  await appearance.open();
  const rendered = await contentAndMedia();
  const colourFor = (scheme: string) =>
    rendered.find(([, media]) => media === `(prefers-color-scheme: ${scheme})`)?.[0];
  const light = colourFor('light');
  const dark = colourFor('dark');
  expect(light).toBeTruthy();
  expect(dark).toBeTruthy();

  await appearance.mode('Dark').check();
  await page.reload();

  await appearance.mode('Light').check();
  await expect.poll(contentAndMedia).toEqual([
    [light, null],
    [light, null],
  ]);

  await appearance.mode('Dark').check();
  await expect.poll(contentAndMedia).toEqual([
    [dark, null],
    [dark, null],
  ]);

  await appearance.mode('System').check();
  await expect.poll(contentAndMedia).toEqual(rendered);
});

test('without JavaScript the colour scheme cannot be changed @nojs', async ({ appearance }) => {
  await appearance.open();

  await expect(appearance.noScriptNote()).toBeVisible();
  await expect(appearance.mode('System')).toBeChecked();
  await expect(appearance.mode('Dark')).toBeDisabled();
});
