import { archivedPlants as archived, people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('archived plants are listed most recently archived first', async ({ archivedPlants }) => {
  await archivedPlants.open();

  await expect(archivedPlants.rows()).toContainText([archived.barry.name, archived.kev.name, archived.sweetBasil.name]);
  await expect(archivedPlants.row(archived.barry.name)).toContainText('Archived');
});

test("an archived plant's page says when it was archived and offers no care or editing", async ({
  archivedPlants,
  plant,
  page,
}) => {
  await archivedPlants.open();
  await archivedPlants.row(archived.kev.name).click();

  await expect(plant.heading()).toHaveText(archived.kev.name);
  await expect(page.getByText(/^Archived \d+ \w+/)).toBeVisible();
  await expect(page.getByRole('link', { name: 'Log care' })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Edit plant' })).toHaveCount(0);
});

test("an archived plant's page goes back to Archived plants", async ({ archivedPlants, page }) => {
  await archivedPlants.open();
  await archivedPlants.row(archived.kev.name).click();

  await page.getByRole('link', { name: 'Archived plants' }).click();

  await expect(page).toHaveURL('/plants/archived');
});

test('an archived plant is restored from its page and listed on Plants again', async ({
  archivedPlants,
  plant,
  plants,
  page,
}) => {
  await archivedPlants.open();
  await archivedPlants.row(archived.barry.name).click();

  await plant.restore();

  await expect(page).toHaveURL(`/plants/${archived.barry.id}`);
  await expect(page.getByRole('link', { name: 'Log care' })).toBeVisible();
  await plants.open();
  await expect(plants.room(archived.barry.room).getByRole('link', { name: archived.barry.name })).toBeVisible();
  await expect(plants.archiveLink()).toHaveText('2 archived');
});

test('a plant archived from its page appears in the archive @swap', async ({ plant, archivedPlants }) => {
  await plant.open(seeded.doris);
  await plant.archive();

  await archivedPlants.open();

  await expect(archivedPlants.row(seeded.doris.name)).toBeVisible();
});
