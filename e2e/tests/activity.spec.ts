import { people } from '../harness/garden';
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
