import { people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// Which plants appear on the first page depends on the day the suite runs,
// because the seed derives its history from the schedules.

// A day marker is the day, a dot, and the number of events that day.
const dayMarker = /^\s*(Today|Yesterday|\w+day|\d{1,2} \w+( \d{4})?)\s*·\s*\d+\s*$/;

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('the Activity tab opens the activity page', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('link', { name: 'Activity' }).click();

  await expect(page).toHaveURL('/activity');
  await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible();
});

test('the activity page starts with the day of the newest event', async ({ activity }) => {
  await activity.open();

  await expect(activity.items().first()).toHaveText(dayMarker);
});

test('an event shows the plant, who did it and when', async ({ activity }) => {
  await activity.open();

  await expect(activity.items().nth(1)).toHaveText(
    /^\s*\S.*\s+\w+ (watered|fed|repotted|misted|pruned|skipped)\s*·\s*\d{1,2}:\d{2}(am|pm)/,
  );
});

test('a plant links to its own activity, and the log links back to the plant', async ({ page, plant, activity }) => {
  await plant.open(seeded.bigFella);

  await plant.allActivity().click();

  await expect(page).toHaveURL(`/activity?plant=${seeded.bigFella.id}`);
  await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible();
  // On a log filtered to one plant, a row is headed by the care, since the
  // plant's name would be the same on every row.
  await expect(activity.items().first()).toHaveText(/^\s*(Watered|Fed|Repotted|Misted|Pruned|Skipped)\s+\w+\s*·/);

  await activity.backTo(seeded.bigFella).click();

  await expect(page).toHaveURL(`/plants/${seeded.bigFella.id}`);
});

test('older activity opens the page below the newest, and latest activity returns to it', async ({
  page,
  activity,
}) => {
  await activity.open();
  const newest = (await activity.items().first().textContent()) ?? '';

  await expect(activity.latest()).toHaveCount(0);

  await activity.older().click();

  await expect(page).toHaveURL(/\/activity\?before=/);
  await expect(activity.items().first()).not.toHaveText(newest);

  await activity.latest().click();

  await expect(page).toHaveURL('/activity');
  await expect(activity.items().first()).toHaveText(newest);
});

test('a row on the activity log opens a sheet filled in from the event', async ({ page, activity, sheet }) => {
  await page.goto(`/activity?plant=${seeded.bigFella.id}`);

  await activity.openSheet(activity.rows().first());

  await expect(sheet.dialog()).toContainText(seeded.bigFella.name);
  await expect(sheet.dialog().getByRole('button', { name: 'Save changes' })).toBeVisible();
  await expect(sheet.deleteButton()).toBeVisible();
  await expect(sheet.recorded()).toBeVisible();
});

test('a correction files the event under the day it was saved with @swap', async ({ activity, sheet }) => {
  await activity.open();
  await activity.openSheet(activity.rows().first());

  await sheet.chip('Yesterday').check();
  await sheet.time().fill('07:07');
  await sheet.submit('Save changes');
  await expect(sheet.dialog()).toHaveCount(0);

  const items = await activity.items().allTextContents();
  const corrected = items.findIndex((item) => item.includes('7:07am'));
  expect(corrected).toBeGreaterThan(0);
  const day = items
    .slice(0, corrected)
    .reverse()
    .find((item) => dayMarker.test(item));
  expect(day).toContain('Yesterday');
});

test('an event recorded as the wrong care is corrected to the right one @swap', async ({ page, activity, sheet }) => {
  await page.goto(`/activity?plant=${seeded.bigFella.id}`);
  await activity.openSheet(activity.rows().first());

  await sheet.what('Feed').click();
  // The chip fetches the sheet again, and the Save button carries the care it
  // will record, so the submit has to wait for the chip that came back.
  await expect(sheet.what('Feed')).toHaveAttribute('aria-pressed', 'true');
  await sheet.submit('Save changes');

  // The log filtered to one plant heads each row with the care rather than the
  // plant's name.
  await expect(activity.rows().first()).toContainText('Fed');
});

test('a deleted event is not on the log @swap', async ({ page, activity, sheet }) => {
  await activity.open();
  const row = activity.rows().first();
  const id = await row.getAttribute('id');

  await activity.openSheet(row);
  await sheet.deleteButton().click();
  // The delete closes the sheet either way: as an out-of-band swap with
  // JavaScript, and as the redirect to the log without it.
  await expect(sheet.dialog()).toHaveCount(0);

  await activity.open();
  await expect(page.locator(`#${id}`)).toHaveCount(0);
});

test('undoing a delete puts the event back on the log @js', async ({ page, activity, sheet }) => {
  await activity.open();
  const row = activity.rows().first();
  const id = await row.getAttribute('id');

  await activity.openSheet(row);
  await sheet.deleteButton().click();
  await expect(page.locator(`#${id}`)).toContainText('Deleted');

  await activity.undo(page.locator(`#${id}`)).click();

  await expect(page.locator(`#${id}`)).not.toContainText('Deleted');
  await activity.open();
  await expect(page.locator(`#${id}`)).toHaveCount(1);
});
