import { browsers, people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('turning both types off makes the More index say notifications are off', async ({ more, notifications }) => {
  await more.open();
  await expect(more.row('Notifications')).not.toContainText('Off');
  await more.row('Notifications').click();

  await notifications.digest().uncheck();
  await notifications.save().click();

  // The hour belongs to the digest, so it leaves the page with it.
  await expect(notifications.hour()).toHaveCount(0);
  await more.open();
  await expect(more.row('Notifications')).toContainText('Off');
});

test('the digest hour is saved and shown on the way back', async ({ notifications }) => {
  await notifications.open();

  await notifications.hour().selectOption('19');
  await notifications.save().click();

  await expect(notifications.hour()).toHaveValue('19');
});

test('a subscribed browser can be removed', async ({ page, notifications }) => {
  await notifications.open();
  const rows = page.getByRole('listitem').filter({ hasText: browsers.phone });
  await expect(rows).toHaveCount(1);

  await rows.getByRole('button', { name: 'Remove' }).click();

  await expect(rows).toHaveCount(0);
  await expect(notifications.digest()).toBeChecked();
});
