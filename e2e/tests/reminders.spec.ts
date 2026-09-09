import { people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// The Reminders page is reached at the end of setup and of joining through an
// invite link. The setup and invite tests cover those routes. These open it
// directly as a signed-in account, since the page only needs a session on a
// garden.
//
// The banner on Today is not here. It appears only when the browser reports
// itself installed and notification permission is still default, and
// Playwright's Chromium reports neither. The server's half of it, the hidden
// element and when it is rendered, is a Go handler test.

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('Turn on notifications in a browser that refuses permission says notifications are blocked and stays on the page @push', async ({
  page,
  reminders,
}) => {
  await reminders.open();

  await reminders.turnOn().click();

  await expect(reminders.refusal()).toContainText('Notifications are blocked on this device');
  await expect(page).toHaveURL('/setup/reminders');
  await reminders.notNow().click();
  await expect(page).toHaveURL('/');
});

// Only the phone project runs this test, because its WebKit has no push API.
test('an iPhone that has not installed sprig is shown the install steps instead of the offer @js', async ({
  page,
  reminders,
}) => {
  await reminders.open();

  await expect(page.getByRole('heading', { name: 'Notifications need the app' })).toBeVisible();
  await expect(reminders.steps().first()).toContainText('Safari');
  await expect(reminders.turnOn()).toBeHidden();

  await reminders.continueLink().click();
  await expect(page).toHaveURL('/');
});

test('with no JavaScript Turn on notifications opens the Notifications page @nojs', async ({ page, reminders }) => {
  await reminders.open();

  await reminders.turnOn().click();

  await expect(page).toHaveURL('/more/notifications');
});
