import { browsers, people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('turning both types off makes the More index say notifications are off @push', async ({ more, notifications }) => {
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

test('a saved digest hour is shown when the page is opened again @push', async ({ notifications }) => {
  await notifications.open();

  await notifications.hour().selectOption('19');
  await notifications.saveChanges();

  await notifications.open();
  await expect(notifications.hour()).toHaveValue('19');
});

// The same flow with JavaScript off, where Save changes is a plain form post
// and a redirect.
test('a digest hour saved with no JavaScript is shown when the page is opened again @nojs', async ({
  notifications,
}) => {
  await notifications.open();

  await notifications.hour().selectOption('19');
  await notifications.saveChanges();

  await notifications.open();
  await expect(notifications.hour()).toHaveValue('19');
});

test('a subscribed browser can be removed @push', async ({ page, notifications }) => {
  await notifications.open();
  const rows = page.getByRole('listitem').filter({ hasText: browsers.phone });
  await expect(rows).toHaveCount(1);

  await rows.getByRole('button', { name: 'Remove' }).click();

  await expect(rows).toHaveCount(0);
  await expect(notifications.digest()).toBeChecked();
});

test('adding this device in a browser that refuses permission says notifications are blocked @push', async ({
  notifications,
}) => {
  await notifications.open();

  await notifications.addDevice().click();

  await expect(notifications.refusal()).toContainText('Notifications are blocked on this device');
});

// Only the push API can make a subscription, so the button needs JavaScript.
test('Add this device is not offered with no JavaScript @nojs', async ({ notifications }) => {
  await notifications.open();

  await expect(notifications.sendTest()).toBeVisible();
  await expect(notifications.addDevice()).toBeHidden();
});

// Only the phone project runs this test, because its WebKit has no push API.
test('an iPhone that has not installed sprig is offered Install sprig instead of the switches @js', async ({
  page,
  notifications,
}) => {
  await notifications.open();

  await expect(notifications.install()).toBeVisible();
  await expect(notifications.digest()).toBeHidden();
  await expect(notifications.addDevice()).toBeHidden();
  await expect(page.getByRole('listitem').filter({ hasText: browsers.phone })).toBeVisible();

  await notifications.install().click();
  await expect(page).toHaveURL('/install');
});

test('a test sent from a browser that has never subscribed says this device is not subscribed @push', async ({
  notifications,
  page,
}) => {
  await notifications.open();

  await notifications.sendTest().click();

  await expect(page.getByRole('main').getByText('This device isn’t subscribed')).toBeVisible();
});

// With JavaScript off the form posts with no endpoint, because only the push
// API knows the browser's own subscription.
test('a test sent with no JavaScript says this device is not subscribed @nojs', async ({ notifications, page }) => {
  await notifications.open();

  await notifications.sendTest().click();

  await expect(page.getByRole('main').getByText('This device isn’t subscribed')).toBeVisible();
});
