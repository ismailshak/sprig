import { people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// The garden's feeding schedules run March to September, so what a feeding row
// says on the right depends on the month the suite runs in. Every claim below
// is about a watering, a rule, or a care recorded during the test.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('a plant on the roster opens its own page', async ({ page, plants, plant }) => {
  await plants.open();

  await plants.row(seeded.bigFella).click();

  await expect(page).toHaveURL(`/plants/${seeded.bigFella.id}`);
  await expect(plant.heading()).toHaveText(seeded.bigFella.name);
});

test('a plant states the names it has beyond the one it goes by', async ({ page, plant }) => {
  await plant.open(seeded.bigFella);

  await expect(plant.heading()).toHaveText('Big Fella');
  await expect(page.getByText('Swiss cheese plant · Monstera deliciosa')).toBeVisible();
  await expect(page.getByText('Living room', { exact: true })).toBeVisible();
});

test('a plant with only a botanical name is led by it', async ({ plant }) => {
  await plant.open(seeded.opuntia);

  await expect(plant.heading()).toHaveText(seeded.opuntia.name);
});

test('a schedule row states its rule and how late the care is', async ({ plant }) => {
  await plant.open(seeded.bigFella);

  await expect(plant.scheduleRow('Water')).toContainText('Every 10 days');
  await expect(plant.scheduleRow('Water')).toContainText('2 days late');
  await expect(plant.scheduleRow('Feed')).toContainText('Every 3 weeks · Mar–Sep');
  await expect(plant.scheduleRow('Repot')).toContainText('Just once');
  await expect(plant.scheduleRow('Repot')).toContainText(/Due in March/);
});

// Sprout carries a nickname and nothing else: no common or botanical name, no
// room, none of the six reference facts, no note and no acquired date.
test('a plant with one name and nothing written down has no reference', async ({ page, plant }) => {
  await plant.open(seeded.sprout);

  await expect(plant.heading()).toHaveText(seeded.sprout.name);
  await expect(plant.section('Reference')).toHaveCount(0);
  // Sun, Soil, Climate, Pot and Acquired appear nowhere else on the page, so
  // their absence is checked against the whole of it. Water and Feed are left
  // out because they are also care types, which the schedule above names.
  for (const label of ['Sun', 'Soil', 'Climate', 'Pot', 'Acquired']) {
    await expect(page.getByText(label, { exact: true })).toHaveCount(0);
  }

  await expect(plant.scheduleRow('Water')).toContainText('Every 11 days');
  await expect(plant.section('Photos')).toBeVisible();
  await expect(plant.recentLines().first()).toContainText('watered');
});

// Big Fella is the plant every reference field is set on, so the six labels
// read here in the one order every plant page uses.
test('a plant carries what has been written down about it', async ({ plant }) => {
  await plant.open(seeded.bigFella);

  await expect(plant.referenceLabels()).toHaveText(['Sun', 'Water', 'Feed', 'Soil', 'Climate', 'Pot']);
  for (const value of await plant.referenceValues().allTextContents()) {
    expect(value.trim()).not.toBe('');
  }
  await expect(plant.section('Reference')).toContainText('Bright indirect. The west window scorches him by August.');
  await expect(plant.section('Reference')).toContainText('Wipe the leaves when they dust over.');
  await expect(plant.section('Reference')).toContainText('Acquired');
  await expect(plant.section('Reference')).toContainText('March 2024');
});

test('recent names the person and the action and not the plant', async ({ plant }) => {
  await plant.open(seeded.bigFella);

  await expect(plant.recentLines().first()).toContainText(/watered|fed|repotted|skipped/);
  await expect(plant.recentLines().first()).not.toContainText(seeded.bigFella.name);
});

// Doris is scheduled for watering alone, so a Repot chip can only have come
// from the garden's care types.
test('the sheet opened from a plant offers every care type in the garden', async ({ plant, sheet }) => {
  await plant.open(seeded.doris);

  await plant.logCare();

  await expect(sheet.dialog()).toHaveAccessibleName(`Log care for ${seeded.doris.name}`);
  await expect(sheet.what('Water')).toHaveAttribute('aria-pressed', 'true');
  await expect(sheet.what('Repot')).toBeVisible();
});

test('the sheet opened from a plant does not lead back to the page it is on', async ({ plant, sheet }) => {
  await plant.open(seeded.doris);

  await plant.logCare();

  await expect(sheet.dialog().getByRole('link')).toHaveCount(0);
});

test('a care logged from a plant lands back on that plant', async ({ page, plant, sheet }) => {
  await plant.open(seeded.doris);
  await plant.logCare();

  await sheet.submit('Log watering');

  await expect(page).toHaveURL(`/plants/${seeded.doris.id}`);
  await expect(plant.recentLines().first()).toContainText('You watered · today');
  await expect(plant.scheduleRow('Water')).toContainText('Due in 21 days');
});

test('a care the plant is not scheduled for is recorded from its own page', async ({ plant, sheet }) => {
  await plant.open(seeded.doris);
  await plant.logCare();

  await sheet.what('Repot').click();
  await sheet.submit('Log repotting');

  await expect(plant.recentLines().first()).toContainText('You repotted · today');
  await expect(plant.scheduleRow('Repot')).toHaveCount(0);
});

test('a time later than now is refused on the plant it was logged from', async ({ plant, sheet }) => {
  await plant.open(seeded.doris);
  await plant.logCare();
  await sheet.chip('Earlier today').check();
  await sheet.time().fill('23:59');

  await sheet.submit('Log watering');

  await expect(sheet.dialog().getByText('That is later than now.')).toBeVisible();
  await expect(plant.heading()).toHaveText(seeded.doris.name);
});
