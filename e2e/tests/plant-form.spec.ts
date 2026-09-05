import { people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// springYear is two years ahead, within the seven years the year select
// offers.
const springYear = String(new Date().getFullYear() + 2);

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test("a new plant's page shows the watering schedule entered on the form", async ({
  page,
  plants,
  plantForm,
  plant,
}) => {
  await plants.open();
  await page.getByRole('link', { name: 'Add', exact: true }).click();
  await page.waitForLoadState();

  await plantForm.field('Nickname').fill('Ada');
  await plantForm.field('Location').fill('Study');
  await plantForm.every('Water').fill('10');
  await plantForm.unit('Water').selectOption('day');
  await plantForm.submit('Add plant');

  await expect(plant.heading()).toHaveText('Ada');
  await expect(plant.scheduleRow('Water')).toContainText('Every 10 days');
  await expect(plant.scheduleRow('Water')).toContainText('Due in 10 days');
});

test('a plant with no name is refused and the location typed is kept', async ({ page, plantForm }) => {
  await plantForm.openNew();
  await plantForm.field('Location').fill('Study');

  await plantForm.submit('Add plant');

  await expect(page.getByText('Give it at least one name. Any of the three will do.')).toBeVisible();
  await expect(plantForm.field('Location')).toHaveValue('Study');
});

test('pressing Enter in a name field adds the plant', async ({ plantForm, plant }) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');

  await plantForm.field('Nickname').press('Enter');

  await expect(plant.heading()).toHaveText('Ada');
});

test('a plant added with no schedule shows every care type as not scheduled', async ({ plantForm, plant }) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');

  await plantForm.dontSchedule('Water');
  await plantForm.submit('Add plant');

  await expect(plant.heading()).toHaveText('Ada');
  for (const care of ['Water', 'Feed', 'Repot']) {
    await expect(plant.scheduleRow(care)).toContainText('Not scheduled');
  }
});

// The When select decides which fields the row shows. Re-rendering the row on
// change is the only thing JavaScript does on this page.
test('a one-off repot shows its month and year as the due date @js', async ({ plantForm, plant }) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');

  await plantForm.schedule('Repot');
  await plantForm.shape('Repot').selectOption({ label: 'Just once' });
  await plantForm.date('Repot', 'month').selectOption({ label: 'March' });
  await plantForm.date('Repot', 'year').selectOption({ label: springYear });
  await plantForm.submit('Add plant');

  await expect(plant.scheduleRow('Repot')).toContainText('Just once');
  await expect(plant.scheduleRow('Repot')).toContainText(`Due in March ${springYear}`);
});

// Without JavaScript the row does not re-render on change, so the post arrives
// without the fields the chosen option needs.
test('a one-off schedule posted without a date is refused until a date is given @nojs', async ({
  page,
  plantForm,
  plant,
}) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');
  await plantForm.schedule('Repot');
  await plantForm.shape('Repot').selectOption({ label: 'Just once' });

  await plantForm.submit('Add plant');

  await expect(page.getByText('Give it a date.')).toBeVisible();
  await plantForm.date('Repot', 'month').selectOption({ label: 'March' });
  await plantForm.date('Repot', 'year').selectOption({ label: springYear });
  await plantForm.submit('Add plant');

  await expect(plant.scheduleRow('Repot')).toContainText(`Due in March ${springYear}`);
});

// Big Fella has all six reference fields set.
test('the edit form shows the reference fields for a plant that has reference notes', async ({ plantForm }) => {
  await plantForm.openEdit(seeded.bigFella);

  await expect(plantForm.field('Sun')).toBeVisible();
  await expect(plantForm.field('Nickname')).toHaveValue('Big Fella');
});

// Sprout has a nickname and nothing else.
test('the edit form hides the reference fields for a plant with no reference notes', async ({ plantForm }) => {
  await plantForm.openEdit(seeded.sprout);

  await expect(plantForm.field('Sun')).toBeHidden();
});

test('a plant is renamed from its own page', async ({ page, plant, plantForm }) => {
  await plant.open(seeded.doris);

  await plant.edit();
  await plantForm.field('Nickname').fill('Doris the Second');
  await plantForm.submit('Save changes');

  await expect(page).toHaveURL(`/plants/${seeded.doris.id}`);
  await expect(plant.heading()).toHaveText('Doris the Second');
});

// The edit form has no schedule section. Schedules are changed on the plant's
// own page.
test('the edit form has no Schedule section', async ({ page, plantForm }) => {
  await plantForm.openEdit(seeded.bigFella);

  await expect(page.getByRole('heading', { name: 'Schedule' })).toHaveCount(0);
});

test('an archived plant is not listed on Plants', async ({ page, plant, plants }) => {
  await plant.open(seeded.doris);

  await plant.archive();

  await expect(page).toHaveURL('/plants');
  await expect(plants.row(seeded.doris)).toHaveCount(0);
});

// Archiving asks for confirmation because it removes the plant from Plants and
// leaves no row to undo from.
test('Archive asks for confirmation and the plant stays listed until it is given', async ({ page, plant, plants }) => {
  await plant.open(seeded.doris);

  await plant.askToArchive();

  await expect(plant.foot()).toContainText(`Archive ${seeded.doris.name}?`);
  await expect(plant.foot()).toContainText('Its history is kept');
  await expect(plant.heading()).toHaveText(seeded.doris.name);
  await plants.open();
  await expect(plants.row(seeded.doris)).toHaveCount(1);
});

test('choosing Keep after Archive leaves the plant listed', async ({ plant, plants }) => {
  await plant.open(seeded.doris);
  await plant.askToArchive();

  await plant.keepIt();

  await expect(plant.foot()).toContainText('Edit plant');
  await plants.open();
  await expect(plants.row(seeded.doris)).toHaveCount(1);
});
