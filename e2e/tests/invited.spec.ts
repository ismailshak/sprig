import type { Browser, BrowserContext, Page } from '@playwright/test';
import { aWorkingDevice, withDevice } from '../harness/authenticator';
import { gardens, invites, people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';
import { AccountScreen } from '../screens/account';
import { AcceptScreen } from '../screens/accept';
import { InvitedScreen } from '../screens/invited';
import { PeopleScreen } from '../screens/people';

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
  await withDevice(page, aWorkingDevice, async () => {
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

    await install.continueLink().click();
    await expect(page).toHaveURL('/');
    await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();

    // The link works once. A second visit gets the page for a link that
    // cannot be used.
    await invited.open(invites.sitter.token);
    await expect(page.getByRole('heading', { name: 'This invite link can’t be used' })).toBeVisible();
  });
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
  try {
    await withDevice(phone, aWorkingDevice, async () => {
      const onPhone = new InvitedScreen(phone);
      await onPhone.open(token);
      await expect(phone.getByRole('heading', { name: 'Add this device to your account' })).toBeVisible();
      await expect(onPhone.name()).toHaveCount(0);

      await onPhone.addDevice().click();

      await expect(phone).toHaveURL('/');
      const account = new AccountScreen(phone);
      await account.open();
      await expect(account.name()).toHaveValue(people.sam.name);
    });
  } finally {
    await context.close();
  }
});

test('an account signed in from an invite link joins the garden and keeps the one it was in @passkey', async ({
  page,
  accept,
  invited,
  passkeys,
  signin,
  today,
}) => {
  await withDevice(page, aWorkingDevice, async () => {
    // Robin owns Upstairs and is not in Home. The seed gives Robin no
    // passkey, so one is registered first and the session signed out.
    await signIn(page, people.robin.handle);
    await passkeys.open();
    await passkeys.add().click();
    await expect(passkeys.rows()).toHaveCount(1);
    await page.goto('/more');
    await page.getByRole('button', { name: 'Sign out' }).click();
    await expect(page).toHaveURL('/signin');

    await invited.open(invites.sitter.token);
    await invited.signInToJoin().click();
    await expect(page).toHaveURL(/^.*\/signin\?next=/);
    await signin.signIn().click();

    await expect(page).toHaveURL(AcceptScreen.pathOf(invites.sitter.token));
    await expect(
      page.getByRole('heading', { name: `Join ${gardens.home.name} as ${people.robin.name}?` }),
    ).toBeVisible();
    await expect(page.getByText('You’ll join as a sitter.')).toBeVisible();

    await accept.join(gardens.home.name).click();

    await expect(page).toHaveURL('/');
    await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();
    await expect(page.getByText(`${people.ellie.name}’s garden`)).toBeVisible();
    await today.switchGarden().click();
    await expect(today.switchTo(gardens.upstairs.name)).toBeVisible();

    // The link works once.
    await accept.open(invites.sitter.token);
    await expect(page.getByRole('heading', { name: 'This invite link can’t be used' })).toBeVisible();
  });
});

test('an account already in the garden is offered Open instead of Join, and the link is not used', async ({
  page,
  accept,
  invited,
}) => {
  await signIn(page, people.ellie.handle);

  await accept.open(invites.sitter.token);

  await expect(page.getByRole('heading', { name: `You’re already in ${gardens.home.name}` })).toBeVisible();
  await expect(page.getByText('This account is already a member. The link hasn’t been used.')).toBeVisible();
  await expect(accept.join(gardens.home.name)).toHaveCount(0);
  await accept.openGarden(gardens.home.name).click();
  await expect(page).toHaveURL('/');
  await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();

  // The link is still unused. Opening it while signed in lands on the accept
  // page again rather than on the join form.
  await invited.open(invites.sitter.token);
  await expect(page).toHaveURL(AcceptScreen.pathOf(invites.sitter.token));
  await expect(page.getByRole('heading', { name: `You’re already in ${gardens.home.name}` })).toBeVisible();
});

test('a signed-in browser opening a join link is sent to accept it as that account', async ({
  page,
  accept,
  invited,
  today,
}) => {
  await signIn(page, people.robin.handle);

  await invited.open(invites.sitter.token);

  await expect(page).toHaveURL(AcceptScreen.pathOf(invites.sitter.token));
  await expect(page.getByRole('heading', { name: `Join ${gardens.home.name} as ${people.robin.name}?` })).toBeVisible();

  await accept.join(gardens.home.name).click();

  await expect(page).toHaveURL('/');
  await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();
  await today.switchGarden().click();
  await expect(today.switchTo(gardens.upstairs.name)).toBeVisible();
});

test('a removed sitter signing in from a join link is back in the garden as the same account @passkey', async ({
  browser,
  baseURL,
  page,
  accept,
  account,
  invited,
  passkeys,
  signin,
  today,
}) => {
  await withDevice(page, aWorkingDevice, async () => {
    // Jo is a sitter in Home with no passkey in the seed, so one is
    // registered first. Then the owner removes Jo, leaving an account in no
    // garden.
    await signIn(page, people.jo.handle);
    await passkeys.open();
    await passkeys.add().click();
    await expect(passkeys.rows()).toHaveCount(1);
    await page.goto('/more');
    await page.getByRole('button', { name: 'Sign out' }).click();
    await expect(page).toHaveURL('/signin');

    const { page: owner, context } = await asAnotherBrowser(browser, baseURL);
    try {
      await signIn(owner, people.ellie.handle);
      const onOwner = new PeopleScreen(owner);
      await onOwner.open();
      await onOwner.remove(people.jo.name).click();
      await onOwner.confirmRemove().click();
      await expect(onOwner.row(people.jo.name)).toHaveCount(0);
    } finally {
      await context.close();
    }

    await invited.open(invites.sitter.token);
    await invited.signInToJoin().click();
    await signin.signIn().click();

    // The sign-in lands on the accept page as Jo with no garden. Join is what
    // puts the account back in the garden.
    await expect(page).toHaveURL(AcceptScreen.pathOf(invites.sitter.token));
    await expect(page.getByRole('heading', { name: `Join ${gardens.home.name} as ${people.jo.name}?` })).toBeVisible();
    await accept.join(gardens.home.name).click();

    await expect(page).toHaveURL('/');
    await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();
    await expect(today.switchGarden()).toHaveCount(0);
    await account.open();
    await expect(account.name()).toHaveValue(people.jo.name);
  });
});

test('an account in no garden opening a join link joins as itself and lands on Today', async ({
  page,
  accept,
  account,
  invited,
  noGarden,
}) => {
  await signIn(page, people.clare.handle);
  await expect(noGarden.heading()).toBeVisible();

  await invited.open(invites.sitter.token);

  await expect(page).toHaveURL(AcceptScreen.pathOf(invites.sitter.token));
  await expect(page.getByRole('heading', { name: `Join ${gardens.home.name} as ${people.clare.name}?` })).toBeVisible();

  await accept.join(gardens.home.name).click();

  await expect(page).toHaveURL('/');
  await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();
  await account.open();
  await expect(account.name()).toHaveValue(people.clare.name);
});

test('a link made on Invite someone joins a new person from another browser @passkey', async ({
  browser,
  baseURL,
  page,
  invite,
  people: peopleScreen,
}) => {
  await signIn(page, people.ellie.handle);
  await peopleScreen.open();
  await peopleScreen.inviteSomeone().click();
  await invite.chip('Member').click();
  await invite.create().click();
  const token = InvitedScreen.tokenOf((await invite.link().textContent()) ?? '');

  const { page: phone, context } = await asAnotherBrowser(browser, baseURL);
  try {
    await withDevice(phone, aWorkingDevice, async () => {
      const onPhone = new InvitedScreen(phone);
      await onPhone.open(token);
      await expect(
        phone.getByRole('heading', { name: `${people.ellie.name} invited you to ${gardens.home.name}` }),
      ).toBeVisible();
      await expect(phone.getByText('You’ll join as a member.')).toBeVisible();
      await onPhone.name().fill('Kim');
      await onPhone.timezone().selectOption('Europe/London');
      await onPhone.join().click();

      await expect(phone).toHaveURL('/install?after=invite');
    });
  } finally {
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

  await expect(page.getByRole('heading', { name: 'This invite link can’t be used' })).toBeVisible();
  await expect(invited.join()).toHaveCount(0);
  await invited.signIn().click();
  await expect(page).toHaveURL('/signin');
});

test('a link that was never issued cannot be used', async ({ page, invited }) => {
  await invited.open('a-token-nobody-made');

  await expect(page.getByRole('heading', { name: 'This invite link can’t be used' })).toBeVisible();
  await expect(invited.join()).toHaveCount(0);
});

test("with no JavaScript the join button is disabled, the page says joining needs JavaScript, and the timezone opens on the inviter's @nojs", async ({
  page,
  invited,
}) => {
  await invited.open(invites.sitter.token);

  await expect(invited.join()).toBeDisabled();
  await expect(page.getByText('Requires JavaScript and a browser with passkey support')).toBeVisible();
  await expect(invited.timezone()).toHaveValue('Europe/London');
});

test.describe('in a browser set to Tokyo', () => {
  test.use({ timezoneId: 'Asia/Tokyo' });

  test("the timezone select opens on the browser's own zone rather than the inviter's @js", async ({ invited }) => {
    await invited.open(invites.sitter.token);

    await expect(invited.timezone()).toHaveValue('Asia/Tokyo');
  });
});
