import { people, plants } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// The seeded feeding schedules are off between October and February, so which
// plants they make due depends on the season. Every assertion below is about a
// watering, which runs all year.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('the Remind me again banner confirms a chosen delay and hides on Dismiss @swap', async ({ today }) => {
  await today.openFromNotification();
  await expect(today.remindIn('In 1 hour')).toBeVisible();

  await today.remindIn('In 1 hour').click();

  await expect(today.remindAgain()).toContainText('You’ll get this notification again at');
  await expect(today.remindIn('In 1 hour')).toHaveCount(0);

  await today.dismissRemindAgain().click();

  await expect(today.remindAgain()).toBeHidden();
});

test('a time already passed is refused and the banner still offers the delays @swap', async ({ today }) => {
  await today.openFromNotification();

  // Midnight is never later than now on the same day.
  await today.remindAt().fill('00:00');
  await today.remindMe().click();

  await expect(today.remindAgain()).toContainText('That time has already passed.');
  await expect(today.remindIn('In 2 hours')).toBeVisible();
});
