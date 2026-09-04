import { people, plants } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// The seeded feeds are shut between October and February, so which plants they
// make due moves with the season. Every claim below is about a watering, which
// runs all year.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('an overdue plant says how late it is', async ({ today }) => {
  await today.open();

  const row = today.careRow(plants.bigFella, 'water');
  await expect(today.section('Overdue').locator(row)).toBeVisible();
  await expect(row).toContainText('2 days late');
  await expect(row.getByRole('button', { name: 'Water' })).toBeVisible();
});

test('a plant due today gives its location and nothing else', async ({ today }) => {
  await today.open();

  const row = today.careRow(plants.doris, 'water');
  await expect(today.section('Due today').locator(row)).toBeVisible();
  await expect(row).toHaveText(/^\s*Doris\s*Bedroom\s*Water\s*$/);
});

test('a plant coming up gives the day and a quiet Log', async ({ today }) => {
  await today.open();

  const row = today.careRow(plants.trailMix, 'water');
  await expect(today.section('Coming up').locator(row)).toBeVisible();
  await expect(row).toContainText('Water tomorrow');
  await expect(row.getByRole('button', { name: 'Log' })).toBeVisible();
});

test('the summary counts what is overdue', async ({ page, today }) => {
  await today.open();

  await expect(page.getByRole('heading', { name: 'Home' })).toBeVisible();
  await expect(page.getByText('1 of them overdue')).toBeVisible();
});

test('a plant beyond the week is not on the page', async ({ today }) => {
  await today.open();

  await expect(today.careRow(plants.motherInLaw, 'water')).toHaveCount(0);
});
