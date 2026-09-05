import { people, plants as seeded, rooms } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// The roster's one standing holds whatever month the suite runs in because Big
// Fella's watering is the only overdue care in Ellie's garden and a watering
// has no season.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('the plants tab opens the roster', async ({ page }) => {
  await page.getByRole('link', { name: 'Plants' }).click();

  await expect(page).toHaveURL('/plants');
  await expect(page.getByRole('heading', { name: 'Plants' })).toBeVisible();
});

test('rooms are alphabetical with the unplaced last', async ({ plants }) => {
  await plants.open();

  await expect(plants.roomHeadings()).toHaveText([
    rooms.bathroom,
    rooms.bedroom,
    rooms.kitchen,
    rooms.livingRoom,
    rooms.windowsill,
    rooms.noRoom,
  ]);
});

test('a plant with no location is listed under No room', async ({ plants }) => {
  await plants.open();

  await expect(plants.room(rooms.noRoom).getByRole('link', { name: seeded.sprout.name })).toBeVisible();
});

test('plants inside a room are alphabetical', async ({ plants }) => {
  await plants.open();

  await expect(plants.rowsIn(rooms.kitchen)).toContainText([
    seeded.goldenPothos.name,
    seeded.littleFella.name,
    seeded.trailMix.name,
  ]);
});

test('an overdue plant says how late it is on the roster', async ({ plants }) => {
  await plants.open();

  await expect(plants.row(seeded.bigFella)).toContainText('Water 2 days late');
});

test('a plant that is not overdue gives only its names', async ({ plants }) => {
  await plants.open();

  await expect(plants.row(seeded.gerald)).toHaveText(/^\s*Gerald\s*Fiddle-leaf fig\s*$/);
});

test('a plant down to one name gets no second line', async ({ plants }) => {
  await plants.open();

  await expect(plants.rowsIn(rooms.noRoom)).toHaveText([/^\s*Sprout\s*$/]);
});
