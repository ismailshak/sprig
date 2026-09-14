import { careTypes, gardens, people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test("the garden's name is saved and Today is headed with it @swap", async ({ garden, today, page }) => {
  await garden.open();

  await garden.name().fill('The Roof');
  await garden.saveName().click();

  await expect(page.getByText('Saved', { exact: true })).toBeVisible();
  await today.open();
  await expect(page.getByRole('heading', { name: 'The Roof', level: 1 })).toBeVisible();
});

test('a care type with care logged against it can be turned off and not deleted @swap', async ({
  garden,
  plant,
  page,
}) => {
  await garden.open();
  await garden.row(careTypes.repot).click();

  await expect(page.getByText(/^Used once/)).toBeVisible();
  await expect(garden.drop('Delete')).toHaveCount(0);
  await garden.drop('Turn off').click();

  await expect(garden.row(careTypes.repot)).toContainText('Off');
  await plant.open(seeded.bigFella);
  await expect(plant.scheduleRow(careTypes.repot)).toHaveCount(0);
});

test('an event stays on the log after its care type is turned off @swap', async ({ garden, activity, page }) => {
  await garden.open();
  await garden.row(careTypes.repot).click();
  await garden.drop('Turn off').click();
  await expect(garden.row(careTypes.repot)).toContainText('Off');

  await page.goto(`/activity?plant=${seeded.bigFella.id}`);
  await expect(activity.rows().filter({ hasText: 'Repotted' })).toHaveCount(1);
});

test('a care type that was turned off is offered again once it is back on @swap', async ({ garden, plant }) => {
  await garden.open();
  await garden.row(careTypes.mist).click();

  await garden.drop('Turn on').click();

  await expect(garden.row(careTypes.mist)).not.toContainText('Off');
  await plant.open(seeded.nigel);
  await expect(plant.scheduleRow(careTypes.mist)).toContainText('Not scheduled');
});

test('a renamed care type keeps the schedules it had @swap', async ({ garden, plant }) => {
  await garden.open();
  await garden.row(careTypes.water).click();

  await garden.typeName().fill('Watering');
  await garden.save().click();

  await expect(garden.row('Watering')).toBeVisible();
  await expect(garden.row(careTypes.water)).toHaveCount(0);
  await plant.open(seeded.doris);
  await expect(plant.scheduleRow('Watering')).toContainText('Every 3 weeks');
});

test('a care type nothing has been logged against is added and then deleted @swap', async ({ garden, page }) => {
  await garden.open();
  await garden.addType().click();

  await garden.typeName().fill('Prune');
  await garden.save().click();
  await expect(garden.row('Prune')).toBeVisible();

  await garden.row('Prune').click();
  await expect(page.getByText('Not used yet, so it can be deleted.')).toBeVisible();
  await garden.drop('Delete').click();

  await expect(garden.row('Prune')).toHaveCount(0);
});

test('a name another care type already has is refused and the page says which @swap', async ({ garden, page }) => {
  await garden.open();
  await garden.addType().click();

  await garden.typeName().fill('water');
  await garden.save().click();

  await expect(page.getByRole('main').getByText('There is already a care type called Water.')).toBeVisible();
  await expect(garden.row(careTypes.water)).toHaveCount(1);
});

test('a care type left without a name is refused @swap', async ({ garden, page }) => {
  await garden.open();
  await garden.row(careTypes.feed).click();

  await garden.typeName().fill('');
  await garden.save().click();

  await expect(page.getByRole('main').getByText('Enter a name.')).toBeVisible();
  await garden.cancel().click();
  await expect(garden.row(careTypes.feed)).toBeVisible();
});

test('a garden name typed in lower case is refused and the garden stays', async ({
  garden,
  deleteGarden,
  today,
  page,
}) => {
  await garden.open();
  await garden.deleteGarden().click();

  await deleteGarden.name().fill('home');
  await deleteGarden.confirm().click();

  await expect(page.getByText('That isn’t the garden’s name. Type it exactly as shown.')).toBeVisible();
  await today.open();
  await expect(page.getByRole('heading', { name: gardens.home.name, level: 1 })).toBeVisible();
});

test('deleting the garden leaves the owner on the no-garden page', async ({ deleteGarden, noGarden, page }) => {
  await deleteGarden.open();

  await deleteGarden.name().fill(gardens.home.name);
  await deleteGarden.confirm().click();

  await expect(page).toHaveURL('/');
  await expect(noGarden.heading()).toBeVisible();
});
