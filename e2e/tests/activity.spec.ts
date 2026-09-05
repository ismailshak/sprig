import { people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// Which plants are on the newest page moves with the day the suite runs because
// the seed derives its history from the schedules.

// A day marker reads as the day, a dot, and how many events it holds.
const dayMarker = /^\s*(Today|Yesterday|\w+day|\d{1,2} \w+( \d{4})?)\s*·\s*\d+\s*$/;

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('the activity tab opens the log', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('link', { name: 'Activity' }).click();

  await expect(page).toHaveURL('/activity');
  await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible();
});

test('the log opens on the day of its newest event', async ({ activity }) => {
  await activity.open();

  await expect(activity.items().first()).toHaveText(dayMarker);
});

test('an event says which plant it was, who did it and when', async ({ activity }) => {
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
  // Filtered to one plant a row is headed by the care rather than by the
  // plant's name, which would be the same on every row.
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
