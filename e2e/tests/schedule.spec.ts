import { people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// Big Fella is watered every ten days and was last watered twelve days ago, so
// the seed fixes what the row says before and after the interval changes. Doris
// has only a watering schedule.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('clicking a schedule row opens the editor in place', async ({ plant }) => {
  await plant.open(seeded.bigFella);

  await plant.editSchedule('Water');

  await expect(plant.shape('Water')).toBeVisible();
  await expect(plant.every('Water')).toHaveValue('10');
  await expect(plant.scheduleRow('Water')).toContainText('Counted from the last time it was logged');
});

test('changing the interval updates the due date on the row @swap', async ({ plant }) => {
  await plant.open(seeded.bigFella);
  await plant.editSchedule('Water');

  await plant.every('Water').fill('20');
  await plant.saveSchedule('Water');

  // The last watering date is unchanged, so changing ten days to twenty moves
  // the due date ten days later rather than restarting the count from today.
  await expect(plant.scheduleRow('Water')).toContainText('Every 20 days');
  await expect(plant.scheduleRow('Water')).toContainText('Due in 8 days');
});

test('Cancel leaves the schedule unchanged', async ({ plant }) => {
  await plant.open(seeded.bigFella);
  await plant.editSchedule('Water');
  await plant.every('Water').fill('20');

  await plant.cancelSchedule('Water');

  await expect(plant.scheduleRow('Water')).toContainText('Every 10 days');
  await expect(plant.shape('Water')).toHaveCount(0);
});

test('a care type with no schedule can be given one from its row @swap', async ({ plant }) => {
  await plant.open(seeded.doris);

  await plant.editSchedule('Feed');
  await plant.every('Feed').fill('2');
  await plant.unit('Feed').selectOption('week');
  await plant.saveSchedule('Feed');

  await expect(plant.scheduleRow('Feed')).toContainText('Every 2 weeks');
  await expect(plant.scheduleRow('Feed')).not.toContainText('Not scheduled');
});

test('a removed schedule leaves the care type listed as Not scheduled @swap', async ({ plant }) => {
  await plant.open(seeded.bigFella);

  await plant.editSchedule('Water');
  await plant.removeSchedule('Water');

  await expect(plant.scheduleRow('Water')).toContainText('Not scheduled');
  await expect(plant.scheduleRow('Water')).not.toContainText('Every 10 days');
});

test('Cancel after Remove leaves the schedule unchanged @swap', async ({ plant }) => {
  await plant.open(seeded.bigFella);
  await plant.editSchedule('Water');

  await plant.askToRemoveSchedule('Water');
  await expect(plant.scheduleRow('Water')).toContainText('Remove this schedule?');
  await plant.cancelRemoveSchedule('Water');
  await plant.cancelSchedule('Water');

  await expect(plant.scheduleRow('Water')).toContainText('Every 10 days');
});

test("removing a schedule keeps its past events on the plant's page", async ({ plant }) => {
  await plant.open(seeded.bigFella);
  const recorded = await plant.recentLines().count();

  await plant.editSchedule('Water');
  await plant.removeSchedule('Water');

  await expect(plant.recentLines()).toHaveCount(recorded);
});

// Changing the When select re-renders the row with the fields that option
// needs. That only happens on change with JavaScript. Without it the post
// re-renders the row, and the handler tests cover that.
test('choosing a one-off replaces the interval fields with a date @js', async ({ plant }) => {
  await plant.open(seeded.doris);
  await plant.editSchedule('Repot');

  await plant.shape('Repot').selectOption('once');

  await expect(plant.every('Repot')).toHaveCount(0);
  await expect(plant.date('Repot', 'month')).toBeVisible();
  await expect(plant.scheduleRow('Repot')).toContainText('A single date');
});

test('a one-off with a month and no day is due for the whole month @js', async ({ plant }) => {
  await plant.open(seeded.doris);
  await plant.editSchedule('Repot');
  await plant.shape('Repot').selectOption('once');

  await plant.date('Repot', 'day').selectOption('0');
  await plant.date('Repot', 'month').selectOption('3');
  await plant.date('Repot', 'year').selectOption('2030');
  await plant.saveSchedule('Repot');

  await expect(plant.scheduleRow('Repot')).toContainText('Just once');
  await expect(plant.scheduleRow('Repot')).toContainText('Due in March 2030');
});

test('a saved schedule survives a reload', async ({ page, plant }) => {
  await plant.open(seeded.bigFella);
  await plant.editSchedule('Water');
  await plant.every('Water').fill('20');
  await plant.saveSchedule('Water');
  // With JavaScript the save is a swap that click does not wait for. A reload
  // before the row has changed cancels the request before the save commits.
  await expect(plant.scheduleRow('Water')).toContainText('Every 20 days');

  await page.reload();

  await expect(plant.scheduleRow('Water')).toContainText('Every 20 days');
});
