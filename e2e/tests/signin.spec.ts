import type { Browser, BrowserContext, Locator, Page } from '@playwright/test';
import { attach, aWorkingDevice } from '../harness/authenticator';
import { devices, people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';
import { PasskeysScreen } from '../screens/passkeys';

test('a signed-out visitor is sent to sign in', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveURL('/signin');
  await expect(page.getByRole('heading', { name: 'Sign in to sprig' })).toBeVisible();
});

test('a session survives a reload', async ({ page }) => {
  await signIn(page, people.ellie.handle);

  await page.reload();
  await expect(page).toHaveURL('/');
});

test('with no JavaScript the sign-in button is disabled and the page says it needs JavaScript @nojs', async ({
  page,
  signin,
}) => {
  await signin.open();

  await expect(signin.signIn()).toBeDisabled();
  await expect(page.getByText('Signing in needs JavaScript and a browser that supports passkeys')).toBeVisible();
});

// addedRow is the Passkeys row for the device the test registered. A row is
// named after the browser it was registered from, so the test finds it by
// excluding the two seeded devices rather than by name.
function addedRow(passkeys: PasskeysScreen): Locator {
  return passkeys.rows().filter({ hasNotText: devices.phone }).filter({ hasNotText: devices.laptop });
}

// registerAndSignOut signs in through the development sign-in, registers a
// passkey on the attached device, and signs out again.
async function registerAndSignOut(page: Page, passkeys: PasskeysScreen) {
  await signIn(page, people.ellie.handle);
  await passkeys.open();
  await passkeys.add().click();
  await expect(addedRow(passkeys)).toBeVisible();
  await page.goto('/more');
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL('/signin');
}

test('a passkey registered on this device signs in @passkey', async ({ page, passkeys, signin }) => {
  const detach = await attach(page, aWorkingDevice);
  try {
    await registerAndSignOut(page, passkeys);

    await signin.signIn().click();

    await expect(page).toHaveURL('/');
  } finally {
    await detach();
  }
});

test('a device with no passkey signs nobody in, and the page says the browser does not say why @passkey', async ({
  page,
  signin,
}) => {
  const detach = await attach(page, aWorkingDevice);
  try {
    await signin.open();

    await signin.signIn().click();

    await expect(signin.refusal()).toHaveText(
      'Nothing signed in. Either you cancelled, or this device holds no passkey for sprig. The browser does not say which.',
    );
    await expect(page).toHaveURL('/signin');
    await expect(signin.signIn()).toBeEnabled();
  } finally {
    await detach();
  }
});

// asAnotherBrowser opens a second browser context on the same app, so a test
// can act on the account from one device while the other stays signed in. The
// caller closes the context. The browser fixture would otherwise hold it open
// for the rest of the worker.
async function asAnotherBrowser(
  browser: Browser,
  baseURL: string | undefined,
): Promise<{ page: Page; context: BrowserContext }> {
  const context = await browser.newContext({ baseURL });
  return { page: await context.newPage(), context };
}

test('removing a passkey signs out the device that signed in with it @passkey', async ({
  browser,
  baseURL,
  page,
  passkeys,
  signin,
}) => {
  const detach = await attach(page, aWorkingDevice);
  try {
    await registerAndSignOut(page, passkeys);
    await signin.signIn().click();
    await expect(page).toHaveURL('/');

    const { page: laptop, context } = await asAnotherBrowser(browser, baseURL);
    try {
      await signIn(laptop, people.ellie.handle);
      const onLaptop = new PasskeysScreen(laptop);
      await onLaptop.open();
      const added = addedRow(onLaptop);
      await added.getByRole('button', { name: 'Remove' }).click();
      await expect(added).toHaveCount(0);
    } finally {
      await context.close();
    }

    await page.reload();
    await expect(page).toHaveURL('/signin');
  } finally {
    await detach();
  }
});

test('removing the passkey this device signed in with lands on the sign-in page @passkey', async ({
  page,
  passkeys,
  signin,
}) => {
  const detach = await attach(page, aWorkingDevice);
  try {
    await registerAndSignOut(page, passkeys);
    await signin.signIn().click();
    await expect(page).toHaveURL('/');

    await passkeys.open();
    await addedRow(passkeys).getByRole('button', { name: 'Remove' }).click();

    await expect(page).toHaveURL('/signin');
    await expect(page.getByRole('heading', { name: 'Sign in to sprig' })).toBeVisible();
  } finally {
    await detach();
  }
});
