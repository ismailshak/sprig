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

test('older activity opens the page below the newest, and latest activity returns to it @swap', async ({
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

// With JavaScript, htmx handles Back on the activity page and puts the
// earlier log back in place of the one shown.
test('Back after Older activity shows the newest page again', async ({ page, activity }) => {
  await activity.open();
  const newest = (await activity.items().first().textContent()) ?? '';

  await activity.older().click();
  await expect(activity.items().first()).not.toHaveText(newest);

  await page.goBack();

  await expect(page).toHaveURL('/activity');
  await expect(activity.items().first()).toHaveText(newest);
  await expect(activity.older()).toBeVisible();
});

test('a row on the activity log opens a sheet filled in from the event', async ({ page, activity, sheet }) => {
  await page.goto(`/activity?plant=${seeded.bigFella.id}`);

  await activity.openSheet(activity.rows().first());

  await expect(sheet.dialog()).toContainText(seeded.bigFella.name);
  await expect(sheet.dialog().getByRole('button', { name: 'Save changes' })).toBeVisible();
  await expect(sheet.deleteButton()).toBeVisible();
  await expect(sheet.loggedBy()).toBeVisible();
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

test('an event logged as the wrong care is corrected to the right one @swap', async ({ page, activity, sheet }) => {
  await page.goto(`/activity?plant=${seeded.bigFella.id}`);
  await activity.openSheet(activity.rows().first());

  await sheet.chip('Feed').check();
  await sheet.submit('Save changes');

  // The log filtered to one plant heads each row with the care rather than the
  // plant's name.
  await expect(activity.rows().first()).toContainText('Fed');
});

test('saving a correction leaves focus on the corrected row without scrolling @js', async ({
  page,
  activity,
  sheet,
}) => {
  await activity.open();
  const row = activity.rows().nth(12);
  const id = await row.getAttribute('id');
  await row.scrollIntoViewIfNeeded();

  await activity.openSheet(row);
  await sheet.submit('Save changes');
  await expect(sheet.dialog()).toHaveCount(0);

  const corrected = page.locator(`#${id}`).getByRole('link');
  await expect(corrected).toBeFocused();
  await expect(corrected).toBeInViewport();
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

test('deleting a care from Activity puts the plant back in Overdue on Today @swap', async ({
  today,
  activity,
  sheet,
}) => {
  await today.open();
  const row = today.careRow(seeded.bigFella, 'water');
  await expect(today.section('Overdue').locator(row)).toBeVisible();
  await today.careButton(seeded.bigFella, 'water').click();
  await expect(row.getByRole('button', { name: 'Water' })).toHaveCount(0);

  // The seed writes nothing dated today, so the care just logged is the
  // newest row.
  await activity.open();
  const logged = activity.rows().first();
  await expect(logged).toContainText(seeded.bigFella.name);
  await activity.openSheet(logged);
  await sheet.deleteButton().click();
  await expect(sheet.dialog()).toHaveCount(0);

  await today.open();
  await expect(today.section('Overdue').locator(row)).toBeVisible();
  await expect(row).toContainText('2 days late');
  await expect(row.getByRole('button', { name: 'Water' })).toBeVisible();
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

test('the log filtered to one care lists that care alone @swap', async ({ page, activity }) => {
  await activity.open();
  await expect(activity.clear()).toHaveCount(0);

  await activity.showFilters();
  await activity.care().selectOption({ label: 'Feed' });
  await activity.apply();

  await expect(page).toHaveURL(/\/activity\?.*care=feed/);
  const rows = await activity.rows().allTextContents();
  expect(rows.length).toBeGreaterThan(0);
  for (const row of rows) {
    expect(row).toContain('fed');
  }
  await expect(activity.care()).toHaveValue('feed');

  await activity.clear().click();

  await expect(page).toHaveURL('/activity');
});

test('a date range with nothing in it says nothing matches @swap', async ({ page, activity }) => {
  await activity.open();

  await activity.showFilters();
  await activity.from().fill('2000-01-01');
  await activity.to().fill('2000-01-31');
  await activity.apply();

  await expect(page).toHaveURL(/\/activity\?.*from=2000-01-01/);
  await expect(page.getByText('Nothing matches these filters')).toBeVisible();
  await expect(activity.rows()).toHaveCount(0);
});
