import { people, plants as seeded, rooms } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// Plants shows one overdue marker in any month, because Big Fella's watering is
// the only overdue care in Ellie's garden and watering has no season.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('the Plants tab opens the plant list', async ({ page }) => {
  await page.getByRole('link', { name: 'Plants' }).click();

  await expect(page).toHaveURL('/plants');
  await expect(page.getByRole('heading', { name: 'Plants' })).toBeVisible();
});

test('rooms are listed alphabetically with No room last', async ({ plants }) => {
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

test('a plant with no room is listed under No room', async ({ plants }) => {
  await plants.open();

  await expect(plants.room(rooms.noRoom).getByRole('link', { name: seeded.sprout.name })).toBeVisible();
});

test('plants within a room are listed alphabetically', async ({ plants }) => {
  await plants.open();

  await expect(plants.rowsIn(rooms.kitchen)).toContainText([
    seeded.goldenPothos.name,
    seeded.littleFella.name,
    seeded.trailMix.name,
  ]);
});

test('an overdue plant shows how late it is on Plants', async ({ plants }) => {
  await plants.open();

  await expect(plants.row(seeded.bigFella)).toContainText('Water 2 days late');
});

test('a plant that is not overdue shows only its names', async ({ plants }) => {
  await plants.open();

  await expect(plants.row(seeded.gerald)).toHaveText(/^\s*Gerald\s*Fiddle-leaf fig\s*$/);
});

test('a plant with one name has no second line', async ({ plants }) => {
  await plants.open();

  await expect(plants.rowsIn(rooms.noRoom)).toHaveText([/^\s*Sprout\s*$/]);
});

test('the Plants page links to Archived plants and says how many there are', async ({ plants, page }) => {
  await plants.open();

  await expect(plants.archivedLink()).toHaveText('3 archived');
  await plants.archivedLink().click();

  await expect(page).toHaveURL('/plants/archived');
  await expect(page.getByRole('heading', { name: 'Archived plants' })).toBeVisible();
});

test('a garden with nothing archived has no archive link', async ({ page, plants }) => {
  await signIn(page, people.robin.handle);

  await plants.open();

  await expect(plants.archivedLink()).toHaveCount(0);
});
