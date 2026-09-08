import { people, plants as seeded, rooms } from '../harness/garden';
import { dimensions, hasExif, heldCount, heldFile, loaded, photos } from '../harness/photos';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// springYear is two years ahead, within the seven years the year select
// offers.
const springYear = String(new Date().getFullYear() + 2);

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test("a new plant's page shows the watering schedule entered on the form @swap", async ({
  page,
  plants,
  plantForm,
  plant,
}) => {
  await plants.open();
  await page.getByRole('link', { name: 'Add', exact: true }).click();
  await page.waitForLoadState();

  await plantForm.field('Nickname').fill('Ada');
  await plantForm.field('Room').fill('Study');
  await plantForm.every('Water').fill('10');
  await plantForm.unit('Water').selectOption('day');
  await plantForm.submit('Add plant');

  await expect(plant.heading()).toHaveText('Ada');
  await expect(plant.scheduleRow('Water')).toContainText('Every 10 days');
  await expect(plant.scheduleRow('Water')).toContainText('Due in 10 days');
});

test('a plant with no name is refused and the room typed is kept', async ({ page, plantForm }) => {
  await plantForm.openNew();
  await plantForm.field('Room').fill('Study');

  await plantForm.submit('Add plant');

  await expect(page.getByText('Enter at least one name.')).toBeVisible();
  await expect(plantForm.field('Room')).toHaveValue('Study');
});

test('pressing Enter in a name field adds the plant', async ({ plantForm, plant }) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');

  await plantForm.field('Nickname').press('Enter');

  await expect(plant.heading()).toHaveText('Ada');
});

test('a plant added with no schedule shows every care type as not scheduled @swap', async ({ plantForm, plant }) => {
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

  await expect(page.getByText('Choose a date.')).toBeVisible();
  await plantForm.date('Repot', 'month').selectOption({ label: 'March' });
  await plantForm.date('Repot', 'year').selectOption({ label: springYear });
  await plantForm.submit('Add plant');

  await expect(plant.scheduleRow('Repot')).toContainText(`Due in March ${springYear}`);
});

// Big Fella has all six detail fields set.
test('the edit form shows the detail fields for a plant that has details', async ({ plantForm }) => {
  await plantForm.openEdit(seeded.bigFella);

  await expect(plantForm.field('Sun')).toBeVisible();
  await expect(plantForm.field('Nickname')).toHaveValue('Big Fella');
});

// Sprout has a nickname and nothing else.
test('the edit form hides the detail fields for a plant with no details', async ({ plantForm }) => {
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

test('an archived plant is not listed on Plants @swap', async ({ page, plant, plants }) => {
  await plant.open(seeded.doris);

  await plant.archive();

  await expect(page).toHaveURL('/plants');
  await expect(plants.row(seeded.doris)).toHaveCount(0);
});

// Archiving asks for confirmation because it removes the plant from Plants and
// leaves no row to undo from.
test('Archive asks for confirmation and the plant stays listed until it is given @swap', async ({
  page,
  plant,
  plants,
}) => {
  await plant.open(seeded.doris);

  await plant.askToArchive();

  await expect(plant.foot()).toContainText(`Archive ${seeded.doris.name}?`);
  await expect(plant.foot()).toContainText('keeps its activity and photos');
  await expect(plant.heading()).toHaveText(seeded.doris.name);
  await plants.open();
  await expect(plants.row(seeded.doris)).toHaveCount(1);
});

test('choosing Cancel after Archive leaves the plant listed @swap', async ({ plant, plants }) => {
  await plant.open(seeded.doris);
  await plant.askToArchive();

  await plant.cancelArchive();

  await expect(plant.foot()).toContainText('Edit plant');
  await plants.open();
  await expect(plants.row(seeded.doris)).toHaveCount(1);
});

test('typing in Room lists the rooms that match and offers the text as a new room @js', async ({ plantForm }) => {
  await plantForm.openNew();

  await plantForm.field('Room').fill('b');

  // Bathroom and Bedroom both contain a b. The option for a room that does not
  // exist yet is always last.
  await expect(plantForm.roomOptions()).toHaveText([rooms.bathroom, rooms.bedroom, 'Add “b” as a new room']);
});

// The datalist is the list a browser running no script shows. Left on the
// field, it would open the browser's own dropdown over the listbox.
test("the browser's own room list is gone once the listbox opens @js", async ({ plantForm }) => {
  await plantForm.openNew();

  await plantForm.field('Room').focus();

  await expect(plantForm.roomList()).toBeVisible();
  await expect(plantForm.field('Room')).not.toHaveAttribute('list');
});

test('a room picked with the arrow keys is the room the plant is added to @js', async ({ plantForm, plants }) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');
  await plantForm.field('Room').fill('b');

  // The first press lands on Bathroom and the second on Bedroom.
  await plantForm.field('Room').press('ArrowDown');
  await plantForm.field('Room').press('ArrowDown');
  await plantForm.field('Room').press('Enter');
  await plantForm.submit('Add plant');

  await plants.open();
  await expect(plants.room(rooms.bedroom).getByRole('link', { name: 'Ada' })).toBeVisible();
});

test('a plant is added to a new room by choosing the option that names it @js', async ({ plantForm, plants }) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');
  await plantForm.field('Room').fill('Potting shed');

  await plantForm.roomOption('Add “Potting shed” as a new room').click();
  await plantForm.submit('Add plant');

  await plants.open();
  await expect(plants.rowsIn('Potting shed')).toHaveText([/^\s*Ada\s*$/]);
});

test('a room typed in a different case adds the plant to the room the garden already has @swap', async ({
  plantForm,
  plants,
}) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');
  await plantForm.field('Room').fill('bathroom');

  await plantForm.submit('Add plant');

  await plants.open();
  await expect(plants.room(rooms.bathroom).getByRole('link', { name: 'Ada' })).toBeVisible();
  // A second room spelled the other way would be a heading of its own.
  await expect(plants.roomHeadings()).toHaveText([
    rooms.bathroom,
    rooms.bedroom,
    rooms.kitchen,
    rooms.livingRoom,
    rooms.windowsill,
    rooms.noRoom,
  ]);
});

// The photo field is shown by the page's script, because the resize happens
// in the browser.
test('with no JavaScript there is no Add photo button and the plant is still added @nojs', async ({
  plantForm,
  plant,
}) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');

  await expect(plantForm.addPhoto()).toBeHidden();
  await plantForm.submit('Add plant');

  await expect(plant.heading()).toHaveText('Ada');
});

// The bytes are fetched from the app rather than read from the file input,
// so the check covers the whole path from the resize to the stored file.
test('the photo the app serves back is 2048 pixels on its long edge with its EXIF block gone @js', async ({
  page,
  plantForm,
  plant,
}) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');
  await plantForm.choosePhoto('Add photo', photos.gpsTagged);
  await expect(plantForm.photoPreview()).toBeVisible();
  await plantForm.submit('Add plant');
  await expect(plant.heading()).toHaveText('Ada');

  const served = await page.request.get((await plant.picture().getAttribute('src')) ?? '');

  expect(served.ok()).toBe(true);
  const bytes = await served.body();
  expect(dimensions(bytes)).toEqual({ width: 2048, height: 1365 });
  expect(hasExif(bytes)).toBe(false);
});

test('a photo stored sideways with an orientation tag is upright after the resize @js', async ({ plantForm }) => {
  await plantForm.openNew();

  await plantForm.choosePhoto('Add photo', photos.sideways);

  await expect(plantForm.photoPreview()).toBeVisible();
  expect(dimensions(await heldFile(plantForm.photoInput('photo')))).toEqual({ width: 800, height: 1200 });
});

test('a chosen photo puts a 192 pixel square in the photo-square input @js', async ({ plantForm }) => {
  await plantForm.openNew();

  await plantForm.choosePhoto('Add photo', photos.gpsTagged);

  await expect(plantForm.photoPreview()).toBeVisible();
  const square = await heldFile(plantForm.photoInput('photo-square'));
  expect(dimensions(square)).toEqual({ width: 192, height: 192 });
  expect(hasExif(square)).toBe(false);
});

test('Remove after choosing a photo leaves the form with no photo to post @js', async ({ plantForm }) => {
  await plantForm.openNew();
  await plantForm.choosePhoto('Add photo', photos.gpsTagged);
  await expect(plantForm.photoPreview()).toBeVisible();

  await plantForm.removePhoto();

  await expect(plantForm.addPhoto()).toBeVisible();
  await expect(plantForm.photoPreview()).toBeHidden();
  expect(await heldCount(plantForm.photoInput('photo'))).toBe(0);
  expect(await heldCount(plantForm.photoInput('photo-square'))).toBe(0);
});

test('a plant added with a photo chosen lands on its page @js', async ({ plantForm, plant }) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');
  await plantForm.choosePhoto('Add photo', photos.gpsTagged);
  await expect(plantForm.photoPreview()).toBeVisible();

  await plantForm.submit('Add plant');

  await expect(plant.heading()).toHaveText('Ada');
});

test('a second photo chosen through Replace is the one the form posts @js', async ({ plantForm }) => {
  await plantForm.openNew();
  await plantForm.choosePhoto('Add photo', photos.gpsTagged);
  await expect(plantForm.photoPreview()).toBeVisible();

  await plantForm.choosePhoto('Replace', photos.sideways);

  await expect(plantForm.photoPreview()).toBeVisible();
  expect(dimensions(await heldFile(plantForm.photoInput('photo')))).toEqual({ width: 800, height: 1200 });
  expect(dimensions(await heldFile(plantForm.photoInput('photo-square')))).toEqual({ width: 192, height: 192 });
});

test('a file that is not a photo reads "This file couldn’t be opened as a photo." @js', async ({ page, plantForm }) => {
  await plantForm.openNew();

  await plantForm.choosePhoto('Add photo', {
    name: 'notes.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('not a photo'),
  });

  await expect(page.getByText('This file couldn’t be opened as a photo.')).toBeVisible();
  await expect(plantForm.addPhoto()).toBeVisible();
  expect(await heldCount(plantForm.photoInput('photo'))).toBe(0);
  expect(await heldCount(plantForm.photoInput('photo-square'))).toBe(0);
});

test('a form refused for having no name says the photo needs choosing again @js', async ({ page, plantForm }) => {
  await plantForm.openNew();
  await plantForm.choosePhoto('Add photo', photos.gpsTagged);
  await expect(plantForm.photoPreview()).toBeVisible();

  await plantForm.submit('Add plant');

  await expect(page.getByText('Enter at least one name.')).toBeVisible();
  await expect(page.getByText('Choose the photo again.')).toBeVisible();
  await expect(plantForm.addPhoto()).toBeVisible();
  await expect(plantForm.photoPreview()).toBeHidden();
});

test('a plant added with a photo shows it on its page and beside its name on Plants @js', async ({
  plantForm,
  plant,
  plants,
}) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');
  await plantForm.choosePhoto('Add photo', photos.gpsTagged);
  await expect(plantForm.photoPreview()).toBeVisible();

  await plantForm.submit('Add plant');

  await expect(plant.heading()).toHaveText('Ada');
  await expect(plant.picture()).toHaveAccessibleName('Picture of Ada');
  await expect.poll(() => loaded(plant.picture())).toBe(true);
  await plants.open();
  await expect.poll(() => loaded(plants.picture('Ada'))).toBe(true);
});

test("Remove on the edit form takes the picture off the plant's page @js", async ({ plantForm, plant }) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');
  await plantForm.choosePhoto('Add photo', photos.gpsTagged);
  await expect(plantForm.photoPreview()).toBeVisible();
  await plantForm.submit('Add plant');
  await expect.poll(() => loaded(plant.picture())).toBe(true);

  await plant.edit();
  await expect.poll(() => loaded(plantForm.currentPicture())).toBe(true);
  await expect(plantForm.addPhoto()).toBeHidden();
  await plantForm.removePhoto();
  await expect(plantForm.addPhoto()).toBeVisible();
  await plantForm.submit('Save changes');

  await expect(plant.heading()).toHaveText('Ada');
  await expect(plant.picture()).toHaveCount(0);
});
