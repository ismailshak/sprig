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

test("a plant's page shows its other names under the heading", async ({ page, plant }) => {
  await plant.open(seeded.bigFella);

  await expect(plant.heading()).toHaveText('Big Fella');
  await expect(page.getByText('Swiss cheese plant · Monstera deliciosa')).toBeVisible();
  await expect(page.getByText('Living room', { exact: true })).toBeVisible();
});

test('a plant with only a botanical name has it as the heading', async ({ plant }) => {
  await plant.open(seeded.opuntia);

  await expect(plant.heading()).toHaveText(seeded.opuntia.name);
});

test('a schedule row shows its rule and how late the care is', async ({ plant }) => {
  await plant.open(seeded.bigFella);

  await expect(plant.scheduleRow('Water')).toContainText('Every 10 days');
  await expect(plant.scheduleRow('Water')).toContainText('2 days late');
  await expect(plant.scheduleRow('Feed')).toContainText('Every 3 weeks · Mar–Sep');
  await expect(plant.scheduleRow('Repot')).toContainText('Just once');
  await expect(plant.scheduleRow('Repot')).toContainText(/Due in March/);
});

// Sprout has a nickname and nothing else. No common or botanical name, no room,
// none of the six detail facts, no note, no acquired date.
test('a plant with no details has no Details section', async ({ page, plant }) => {
  await plant.open(seeded.sprout);

  await expect(plant.heading()).toHaveText(seeded.sprout.name);
  await expect(plant.section('Details')).toHaveCount(0);
  // Sun, Soil, Climate, Pot and Acquired appear nowhere else on the page, so
  // their absence is checked against the whole page. Water and Feed are skipped
  // because they are also care types named in the Schedule section.
  for (const label of ['Sun', 'Soil', 'Climate', 'Pot', 'Acquired']) {
    await expect(page.getByText(label, { exact: true })).toHaveCount(0);
  }

  await expect(plant.scheduleRow('Water')).toContainText('Every 11 days');
  await expect(plant.section('Photos')).toBeVisible();
  await expect(plant.recentLines().first()).toContainText('watered');
});

// Big Fella has every detail field set, so the six labels appear here in
// the order every plant page uses.
test("a plant's page shows its details", async ({ plant }) => {
  await plant.open(seeded.bigFella);

  await expect(plant.detailLabels()).toHaveText(['Sun', 'Water', 'Feed', 'Soil', 'Climate', 'Pot']);
  for (const value of await plant.detailValues().allTextContents()) {
    expect(value.trim()).not.toBe('');
  }
  await expect(plant.section('Details')).toContainText('Bright indirect. The west window scorches him by August.');
  await expect(plant.section('Details')).toContainText('Wipe the leaves when they dust over.');
  await expect(plant.section('Details')).toContainText('Acquired');
  await expect(plant.section('Details')).toContainText('March 2024');
});

test('Recent lines show who did what without the plant name', async ({ plant }) => {
  await plant.open(seeded.bigFella);

  await expect(plant.recentLines().first()).toContainText(/watered|fed|repotted|skipped/);
  await expect(plant.recentLines().first()).not.toContainText(seeded.bigFella.name);
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
