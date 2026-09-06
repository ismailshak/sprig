import { careTypes, people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test("the garden's name is saved and Today is headed with it", async ({ garden, today, page }) => {
  await garden.open();

  await garden.name().fill('The Roof');
  await garden.saveName().click();

  await expect(garden.name()).toHaveValue('The Roof');
  await today.open();
  await expect(page.getByRole('heading', { name: 'The Roof', level: 1 })).toBeVisible();
});

test('a care type with events recorded against it can be turned off and not deleted', async ({
  garden,
  plant,
  page,
}) => {
  await garden.open();
  await garden.row(careTypes.repot).click();

  await expect(page.getByText(/^Recorded once/)).toBeVisible();
  await expect(garden.drop('Delete')).toHaveCount(0);
  await garden.drop('Turn it off').click();

  await expect(garden.row(careTypes.repot)).toContainText('Off');
  await plant.open(seeded.bigFella);
  await expect(plant.scheduleRow(careTypes.repot)).toHaveCount(0);
});

test('an event stays on the log after its care type is turned off', async ({ garden, activity, page }) => {
  await garden.open();
  await garden.row(careTypes.repot).click();
  await garden.drop('Turn it off').click();

  await page.goto(`/activity?plant=${seeded.bigFella.id}`);
  await expect(activity.rows().filter({ hasText: 'Repotted' })).toHaveCount(1);
});

test('a care type that was turned off is offered again once it is back on', async ({ garden, plant }) => {
  await garden.open();
  await garden.row(careTypes.mist).click();

  await garden.drop('Turn it back on').click();

  await expect(garden.row(careTypes.mist)).not.toContainText('Off');
  await plant.open(seeded.nigel);
  await expect(plant.scheduleRow(careTypes.mist)).toContainText('Not scheduled');
});

test('a renamed care type keeps the schedules it had', async ({ garden, plant }) => {
  await garden.open();
  await garden.row(careTypes.water).click();

  await garden.typeName().fill('Watering');
  await garden.save().click();

  await expect(garden.row('Watering')).toBeVisible();
  await expect(garden.row(careTypes.water)).toHaveCount(0);
  await plant.open(seeded.doris);
  await expect(plant.scheduleRow('Watering')).toContainText('Every 3 weeks');
});

test('a care type nothing has been recorded against is added and then deleted', async ({ garden, page }) => {
  await garden.open();
  await garden.addType().click();

  await garden.typeName().fill('Prune');
  await garden.save().click();
  await expect(garden.row('Prune')).toBeVisible();

  await garden.row('Prune').click();
  await expect(page.getByText('Nothing has been recorded against it yet')).toBeVisible();
  await garden.drop('Delete').click();

  await expect(garden.row('Prune')).toHaveCount(0);
});

test('a name another care type already has is refused and the page says which', async ({ garden, page }) => {
  await garden.open();
  await garden.addType().click();

  await garden.typeName().fill('water');
  await garden.save().click();

  await expect(page.getByText('There is already a care type called Water.')).toBeVisible();
  await expect(garden.row(careTypes.water)).toHaveCount(1);
});

test('a care type left without a name is refused', async ({ garden, page }) => {
  await garden.open();
  await garden.row(careTypes.feed).click();

  await garden.typeName().fill('');
  await garden.save().click();

  await expect(page.getByText('Give the care type a name.')).toBeVisible();
  await garden.cancel().click();
  await expect(garden.row(careTypes.feed)).toBeVisible();
});
