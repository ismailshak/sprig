import { people, plants as seeded } from '../harness/garden';
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

test("the Plants tab on a plant's page goes back to the list", async ({ page, plant }) => {
  await plant.open(seeded.bigFella);

  await page.getByRole('navigation').getByRole('link', { name: 'Plants' }).click();

  await expect(page).toHaveURL('/plants');
  await expect(page.getByRole('heading', { name: 'Plants' })).toBeVisible();
});

test('pressing the Plants tab on the Plants page scrolls it back to the top @js', async ({ page, plants }) => {
  await plants.open();
  await plants.archivedLink().scrollIntoViewIfNeeded();
  await expect(plants.roomHeadings().first()).not.toBeInViewport();

  await page.getByRole('navigation').getByRole('link', { name: 'Plants' }).click();

  await expect(plants.roomHeadings().first()).toBeInViewport();
  await expect(page).toHaveURL('/plants');
});

test('the Plants page links to Archived plants and says how many there are', async ({ plants, page }) => {
  await plants.open();

  await expect(plants.archivedLink()).toHaveText('3 archived');
  await plants.archivedLink().click();

  await expect(page).toHaveURL('/plants/archived');
  await expect(page.getByRole('heading', { name: 'Archived plants' })).toBeVisible();
});
