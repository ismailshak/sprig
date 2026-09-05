import { people, plants } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page, today }) => {
  await signIn(page, people.ellie.handle);
  await today.open();
});

test('a row opens the sheet for its care', async ({ today, sheet }) => {
  await today.openSheet(plants.nigel, 'water');

  await expect(sheet.dialog()).toHaveAccessibleName('Log care for Nigel');
  await expect(sheet.dialog().getByRole('link', { name: /Nigel/ })).toBeVisible();
  await expect(sheet.what('Water')).toHaveAttribute('aria-pressed', 'true');
  await expect(sheet.what('Feed')).toHaveAttribute('aria-pressed', 'false');
  await expect(sheet.chip('Just now')).toBeChecked();
  await expect(sheet.dialog().getByRole('button', { name: 'Log watering' })).toBeVisible();
});

test('a plant with one care is not asked what', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');

  await expect(sheet.dialog()).toBeVisible();
  await expect(sheet.dialog().getByText('What', { exact: true })).toHaveCount(0);
});

test('logging from the sheet takes the plant off the list', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');
  await sheet.submit('Log watering');
  await expect(sheet.dialog()).toHaveCount(0);

  await today.open();
  await expect(today.careRow(plants.doris, 'water')).toHaveCount(0);
});

test('the care button logs the care as now', async ({ today }) => {
  await today.careButton(plants.doris, 'water').click();
  await expect(today.careButton(plants.doris, 'water')).toHaveCount(0);

  await today.open();
  await expect(today.careRow(plants.doris, 'water')).toHaveCount(0);
});

test('a skip asks again in the chosen days', async ({ today, sheet }) => {
  await today.openSheet(plants.nigel, 'water');
  await sheet.chip('Skipped').check();
  await sheet.chip('1 day').check();
  await sheet.submit('Record a skip');
  await expect(sheet.dialog()).toHaveCount(0);

  await today.open();
  const row = today.careRow(plants.nigel, 'water');
  await expect(today.section('Coming up').locator(row)).toBeVisible();
  await expect(row).toContainText('Water tomorrow');
});

test('switching what names that care\'s usual interval', async ({ today, sheet }) => {
  await today.openSheet(plants.nigel, 'water');
  await sheet.chip('Skipped').check();
  await expect(sheet.chip('The usual 4 days')).toBeVisible();

  await sheet.what('Feed').click();

  await expect(sheet.what('Feed')).toHaveAttribute('aria-pressed', 'true');
  await expect(sheet.chip('The usual 21 days')).toBeVisible();
  await expect(sheet.chip('Skipped')).toBeChecked();
  await expect(sheet.dialog().getByRole('button', { name: 'Record a skip' })).toBeVisible();
});

test('a time later than now is refused', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');
  await sheet.chip('Earlier today').check();
  await sheet.time().fill('23:59');
  await sheet.submit('Log watering');

  await expect(sheet.dialog().getByText('That is later than now.')).toBeVisible();
  await expect(sheet.chip('Earlier today')).toBeChecked();
});

test('cancelling the sheet leaves the row as it was', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');
  await sheet.cancel();

  await expect(sheet.dialog()).toBeHidden();
  await expect(today.careRow(plants.doris, 'water')).toBeVisible();
});

test('a logged row says what was recorded without a reload @js', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');
  await sheet.submit('Log watering');

  await expect(today.careRow(plants.doris, 'water')).toContainText('Watered just now');
  await expect(sheet.dialog()).toHaveCount(0);
});
