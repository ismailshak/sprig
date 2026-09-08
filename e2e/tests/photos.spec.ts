import { people, plants as seeded } from '../harness/garden';
import { photos as files } from '../harness/photos';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// The seed has no photos, so every flow that needs one adds it first. Adding
// needs the page's script for the resize, so those flows run with JavaScript
// only.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('a plant with no photos reads "No photos yet" on its Photos page', async ({ page, photos }) => {
  await photos.open(seeded.bigFella);

  await expect(page.getByText('No photos yet')).toBeVisible();
  await expect(photos.addPhoto()).toBeVisible();
  await expect(photos.tiles()).toHaveCount(0);
});

test('a photo added from the plant page is first in the strip and in the grid @js', async ({
  page,
  plant,
  photoForm,
  photos,
}) => {
  await plant.open(seeded.bigFella);
  await expect(plant.stripPhotos()).toHaveCount(0);
  await plant.addPhoto().click();

  await photoForm.choose(files.gpsTagged);
  await expect(photoForm.preview()).toBeVisible();
  await photoForm.submit();

  await expect(page).toHaveURL(`/plants/${seeded.bigFella.id}/photos`);
  await expect(photos.tiles()).toHaveCount(1);
  await expect(photos.addTile()).toBeVisible();
  await photos.back(seeded.bigFella).click();
  await expect(plant.stripPhotos()).toHaveCount(1);
  await expect(plant.stripPhotos().first()).toContainText('today');
});

test("pressing the picture at the top of a plant's page opens that photo's page @js", async ({
  page,
  plantForm,
  plant,
  photo,
}) => {
  await plantForm.openNew();
  await plantForm.field('Nickname').fill('Ada');
  await plantForm.choosePhoto('Add photo', files.gpsTagged);
  await expect(plantForm.photoPreview()).toBeVisible();
  await plantForm.submit('Add plant');
  await expect(plant.heading()).toHaveText('Ada');

  await plant.openPicture();

  await expect(page).toHaveURL(/\/plants\/[^/]+\/photos\/[^/]+$/);
  await expect(photo.picture()).toBeVisible();
  await expect(photo.meta()).toHaveText('Uploaded today by you');
});

test("deleting a photo asks first and then removes it from the plant's page @js", async ({
  page,
  plant,
  photoForm,
  photos,
  photo,
}) => {
  await plant.open(seeded.bigFella);
  await plant.addPhoto().click();
  await photoForm.choose(files.gpsTagged);
  await expect(photoForm.preview()).toBeVisible();
  await photoForm.submit();
  await photos.openTile(0);

  await photo.askToDelete();
  await expect(photo.foot()).toContainText('Delete this photo?');
  await photo.cancelDelete();
  await expect(photo.foot()).not.toContainText('Delete this photo?');
  await photo.delete();

  await expect(page).toHaveURL(`/plants/${seeded.bigFella.id}/photos`);
  await expect(page.getByText('No photos yet')).toBeVisible();
  await plant.open(seeded.bigFella);
  await expect(plant.stripPhotos()).toHaveCount(0);
});

test("a sitter's plant page has no Add tile and says there are no photos", async ({ page, plant }) => {
  await signIn(page, people.jo.handle);

  await plant.open(seeded.bigFella);

  await expect(plant.addPhoto()).toBeHidden();
  await expect(plant.section('Photos')).toContainText('No photos yet.');
});

// The resize happens in the browser, so a browser running no script cannot
// add a photo and the page says so.
test('with no JavaScript Add photo says a photo cannot be added @nojs', async ({ photoForm }) => {
  await photoForm.open(seeded.bigFella);

  await expect(photoForm.unsupported()).toBeVisible();
  await expect(photoForm.chooseButton()).toBeHidden();
  await expect(photoForm.submitButton()).toBeHidden();
});
