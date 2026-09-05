import { people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// Big Fella is watered every ten days and was last watered twelve days ago, so
// the seed fixes what the row says before and after a change to the interval.
// Doris is scheduled for watering alone.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('a schedule row opens as an editor where it is read', async ({ plant }) => {
  await plant.open(seeded.bigFella);

  await plant.editSchedule('Water');

  await expect(plant.shape('Water')).toBeVisible();
  await expect(plant.every('Water')).toHaveValue('10');
  await expect(plant.scheduleRow('Water')).toContainText('Counted from the last time it was done');
});

test('a changed interval moves the due date on the row it was changed on', async ({ plant }) => {
  await plant.open(seeded.bigFella);
  await plant.editSchedule('Water');

  await plant.every('Water').fill('20');
  await plant.saveSchedule('Water');

  // The last watering has not moved, so ten days made twenty pushes the next
  // one ten days out rather than starting the count again from today.
  await expect(plant.scheduleRow('Water')).toContainText('Every 20 days');
  await expect(plant.scheduleRow('Water')).toContainText('Due in 8 days');
});

test('an editor left by Cancel leaves the schedule as it was', async ({ plant }) => {
  await plant.open(seeded.bigFella);
  await plant.editSchedule('Water');
  await plant.every('Water').fill('20');

  await plant.cancelSchedule('Water');

  await expect(plant.scheduleRow('Water')).toContainText('Every 10 days');
  await expect(plant.shape('Water')).toHaveCount(0);
});

test('a care type the plant is not on is listed as not scheduled', async ({ plant }) => {
  await plant.open(seeded.doris);

  await expect(plant.scheduleRow('Feed')).toContainText('Not scheduled');
  await expect(plant.scheduleRow('Repot')).toContainText('Not scheduled');
});

test('a care type the plant is not on acquires a schedule from its own row', async ({ plant }) => {
  await plant.open(seeded.doris);

  await plant.editSchedule('Feed');
  await plant.every('Feed').fill('2');
  await plant.unit('Feed').selectOption('week');
  await plant.saveSchedule('Feed');

  await expect(plant.scheduleRow('Feed')).toContainText('Every 2 weeks');
  await expect(plant.scheduleRow('Feed')).not.toContainText('Not scheduled');
});

test('a removed schedule leaves the care type in the not scheduled list', async ({ plant }) => {
  await plant.open(seeded.bigFella);

  await plant.editSchedule('Water');
  await plant.removeSchedule('Water');

  await expect(plant.scheduleRow('Water')).toContainText('Not scheduled');
  await expect(plant.scheduleRow('Water')).not.toContainText('Every 10 days');
});

test('a removal that is not confirmed leaves the schedule where it is', async ({ plant }) => {
  await plant.open(seeded.bigFella);
  await plant.editSchedule('Water');

  await plant.askToRemoveSchedule('Water');
  await expect(plant.scheduleRow('Water')).toContainText('Remove this schedule?');
  await plant.keepSchedule('Water');
  await plant.cancelSchedule('Water');

  await expect(plant.scheduleRow('Water')).toContainText('Every 10 days');
});

test('the events a removed schedule produced stay on the plant', async ({ plant }) => {
  await plant.open(seeded.bigFella);
  const recorded = await plant.recentLines().count();

  await plant.editSchedule('Water');
  await plant.removeSchedule('Water');

  await expect(plant.recentLines()).toHaveCount(recorded);
});

// Picking a shape redraws the row with the fields that shape needs, which only
// happens as it is picked where a script is running. A browser without one is
// redrawn by the post, which the handler tests cover.
test('a one-off replaces the interval with a date @js', async ({ plant }) => {
  await plant.open(seeded.doris);
  await plant.editSchedule('Repot');

  await plant.shape('Repot').selectOption('once');

  await expect(plant.every('Repot')).toHaveCount(0);
  await expect(plant.date('Repot', 'month')).toBeVisible();
  await expect(plant.scheduleRow('Repot')).toContainText('One date, and then nothing');
});

test('a one-off given a month is due for the whole of it @js', async ({ plant }) => {
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

  await page.reload();

  await expect(plant.scheduleRow('Water')).toContainText('Every 20 days');
});
