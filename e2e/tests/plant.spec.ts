import { people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// The seeded feeding schedules run March to September, so a feeding row's due
// date depends on the month the suite runs in. Every assertion below is about a
// watering, a schedule rule, or a care logged during the test.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('clicking a plant on Plants opens its page', async ({ page, plants, plant }) => {
  await plants.open();

  await plants.row(seeded.bigFella).click();

  await expect(page).toHaveURL(`/plants/${seeded.bigFella.id}`);
  await expect(plant.heading()).toHaveText(seeded.bigFella.name);
});

// Doris has only a watering schedule, so a Repot chip can only come from the
// garden's list of care types.
test('the sheet opened from a plant offers every care type in the garden', async ({ plant, sheet }) => {
  await plant.open(seeded.doris);

  await plant.logCare();

  await expect(sheet.dialog()).toHaveAccessibleName(`Log care for ${seeded.doris.name}`);
  await expect(sheet.chip('Water')).toBeChecked();
  await expect(sheet.chip('Repot')).toBeVisible();
});

test("the sheet opened from a plant's page has no link back to that page", async ({ plant, sheet }) => {
  await plant.open(seeded.doris);

  await plant.logCare();

  await expect(sheet.dialog().getByRole('link')).toHaveCount(0);
});

test("logging a care from a plant's page returns to that page @swap", async ({ page, plant, sheet }) => {
  await plant.open(seeded.doris);
  await plant.logCare();

  await sheet.submit('Log watering');

  await expect(page).toHaveURL(`/plants/${seeded.doris.id}`);
  await expect(plant.recentLines().first()).toContainText('You watered · today');
  await expect(plant.scheduleRow('Water')).toContainText('Due in 21 days');
});

test("a care with no schedule can be logged from the plant's page @swap", async ({ plant, sheet }) => {
  await plant.open(seeded.doris);
  await plant.logCare();

  await sheet.chip('Repot').check();
  await sheet.submit('Log repotting');

  await expect(plant.recentLines().first()).toContainText('You repotted · today');
  await expect(plant.scheduleRow('Repot')).toContainText('Not scheduled');
});

test("a time later than now is refused on the plant's page @swap", async ({ plant, sheet }) => {
  await plant.open(seeded.doris);
  await plant.logCare();
  await sheet.chip('Earlier today').check();
  await sheet.time().fill('23:59');

  await sheet.submit('Log watering');

  await expect(sheet.dialog().getByText('That time is in the future.')).toBeVisible();
  await expect(plant.heading()).toHaveText(seeded.doris.name);
});
