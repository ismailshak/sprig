import type { Browser, BrowserContext, Page } from '@playwright/test';
import { attach, aWorkingDevice } from '../harness/authenticator';
import { gardens, invites, people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';
import { AccountScreen } from '../screens/account';
import { InvitedScreen } from '../screens/invited';

// asAnotherBrowser opens a second browser context on the same app, so a test
// can issue a link as the owner in one and open it in the other. The caller
// closes the context.
async function asAnotherBrowser(
  browser: Browser,
  baseURL: string | undefined,
): Promise<{ page: Page; context: BrowserContext }> {
  const context = await browser.newContext({ baseURL });
  return { page: await context.newPage(), context };
}

test('joining through an invite link signs the sitter in and lands on Install sprig with a way into the garden @passkey', async ({
  page,
  invited,
  install,
}) => {
  const detach = await attach(page, aWorkingDevice);
  try {
    await invited.open(invites.sitter.token);
    await expect(
      page.getByRole('heading', { name: `${people.ellie.name} invited you to ${gardens.home.name}` }),
    ).toBeVisible();
    await invited.name().fill('Kim');
    await invited.timezone().selectOption('Europe/London');
    await invited.join().click();

    await expect(page).toHaveURL('/install?after=invite');
    await expect(page.getByRole('heading', { name: 'Install sprig' })).toBeVisible();
    await expect(page.getByRole('navigation')).toHaveCount(0);

    await install.goToGarden().click();
    await expect(page).toHaveURL('/');
    await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();

    // The link works once. A second visit gets the page for a link that
    // cannot be used.
    await invited.open(invites.sitter.token);
    await expect(page.getByRole('heading', { name: 'This link cannot be used' })).toBeVisible();
  } finally {
    await detach();
  }
});

test("a re-enrolment link adds a passkey to the member's account and signs that device in @passkey", async ({
  browser,
  baseURL,
  page,
  people: peopleScreen,
}) => {
  await signIn(page, people.ellie.handle);
  await peopleScreen.open();
  await peopleScreen.reenrol(people.sam.name).click();
  const token = InvitedScreen.tokenOf((await peopleScreen.link().textContent()) ?? '');

  const { page: phone, context } = await asAnotherBrowser(browser, baseURL);
  const detach = await attach(phone, aWorkingDevice);
  try {
    const onPhone = new InvitedScreen(phone);
    await onPhone.open(token);
    await expect(phone.getByRole('heading', { name: 'Add this device to your account' })).toBeVisible();
    await expect(onPhone.name()).toHaveCount(0);

    await onPhone.addDevice().click();

    await expect(phone).toHaveURL('/');
    const account = new AccountScreen(phone);
    await account.open();
    await expect(account.name()).toHaveValue(people.sam.name);
  } finally {
    await detach();
    await context.close();
  }
});

test('a revoked invite link cannot be used, and the page offers sign in instead', async ({
  page,
  invited,
  people: peopleScreen,
}) => {
  await signIn(page, people.ellie.handle);
  await peopleScreen.open();
  await peopleScreen.revoke().click();
  await expect(peopleScreen.invited()).toHaveCount(0);

  await invited.open(invites.sitter.token);

  await expect(page.getByRole('heading', { name: 'This link cannot be used' })).toBeVisible();
  await expect(invited.join()).toHaveCount(0);
  await invited.signIn().click();
  await expect(page).toHaveURL('/signin');
});

test('a link that was never issued cannot be used', async ({ page, invited }) => {
  await invited.open('a-token-nobody-made');

  await expect(page.getByRole('heading', { name: 'This link cannot be used' })).toBeVisible();
  await expect(invited.join()).toHaveCount(0);
});

test("with no JavaScript the join button is disabled, the page says joining needs JavaScript, and the timezone opens on the inviter's @nojs", async ({
  page,
  invited,
}) => {
  await invited.open(invites.sitter.token);

  await expect(invited.join()).toBeDisabled();
  await expect(page.getByText('Joining needs JavaScript and a browser that supports passkeys')).toBeVisible();
  await expect(invited.timezone()).toHaveValue('Europe/London');
});

test.describe('in a browser set to Tokyo', () => {
  test.use({ timezoneId: 'Asia/Tokyo' });

  test("the timezone select opens on the browser's own zone rather than the inviter's @js", async ({ invited }) => {
    await invited.open(invites.sitter.token);

    await expect(invited.timezone()).toHaveValue('Asia/Tokyo');
  });
});
