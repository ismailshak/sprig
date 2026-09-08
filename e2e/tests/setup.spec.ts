import { attach, aWorkingDevice, withDevice } from '../harness/authenticator';
import { gardens, people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// The e2e stack runs with sign-up on. The seed has already created an account,
// and with sign-up off the page is closed once one exists.

test('setting up a garden signs its owner in and Today says No plants yet @passkey', async ({
  page,
  setup,
  garden,
}) => {
  const detach = await attach(page, aWorkingDevice);
  try {
    await setup.open();
    await setup.garden().fill('Greenhouse');
    await setup.name().fill('Robin');
    await setup.timezone().selectOption('Europe/London');
    await setup.create().click();

    await expect(page).toHaveURL('/');
    await expect(page.getByRole('heading', { name: 'Greenhouse' })).toBeVisible();
    await expect(page.getByText('No plants yet')).toBeVisible();

    // The Garden page is served to the owner alone, so reaching it says the
    // membership is an owner's.
    await garden.open();
    await expect(garden.name()).toHaveValue('Greenhouse');
  } finally {
    await detach();
  }
});

test('an account signed in from Set up your garden gets a garden of its own and keeps the one it was in @passkey', async ({
  page,
  passkeys,
  setup,
  signin,
  today,
}) => {
  await withDevice(page, aWorkingDevice, async () => {
    // Robin owns Upstairs. The seed gives Robin no passkey, so one is
    // registered first and the session signed out.
    await signIn(page, people.robin.handle);
    await passkeys.open();
    await passkeys.add().click();
    await expect(passkeys.rows()).toHaveCount(1);
    await page.goto('/more');
    await page.getByRole('button', { name: 'Sign out' }).click();
    await expect(page).toHaveURL('/signin');

    await setup.open();
    await setup.signInToSetUp().click();
    await expect(page).toHaveURL(/^.*\/signin\?next=/);
    await signin.signIn().click();

    await expect(page).toHaveURL('/setup/signed-in');
    await expect(page.getByRole('heading', { name: `Set up a garden as ${people.robin.name}` })).toBeVisible();
    await expect(setup.name()).toHaveCount(0);
    await setup.garden().fill('Greenhouse');
    await setup.create().click();

    await expect(page).toHaveURL('/');
    await expect(page.getByRole('heading', { name: 'Greenhouse' })).toBeVisible();
    await expect(page.getByText('No plants yet')).toBeVisible();
    await today.switchGarden().click();
    await expect(today.gardenRow('Greenhouse')).toContainText('Owner');
    await expect(today.switchTo(gardens.upstairs.name)).toBeVisible();

    // Setting up a garden this way registers no passkey, so the account still
    // has the one added above.
    await passkeys.open();
    await expect(passkeys.rows()).toHaveCount(1);
  });
});

test('a signed-in browser opening Set up your garden is sent to set one up as that account', async ({
  page,
  setup,
  today,
}) => {
  await signIn(page, people.robin.handle);

  await setup.open();

  await expect(page).toHaveURL('/setup/signed-in');
  await expect(page.getByRole('heading', { name: `Set up a garden as ${people.robin.name}` })).toBeVisible();

  await setup.create().click();

  await expect(page).toHaveURL('/setup/signed-in');
  await expect(page.getByText('Enter a name for your garden.')).toBeVisible();

  await setup.garden().fill('Greenhouse');
  await setup.create().click();

  await expect(page).toHaveURL('/');
  await expect(page.getByRole('heading', { name: 'Greenhouse' })).toBeVisible();
  await today.switchGarden().click();
  await expect(today.switchTo(gardens.upstairs.name)).toBeVisible();
});

test('an account in no garden is told so on every page and sets up a garden of its own', async ({
  page,
  noGarden,
  setup,
  today,
}) => {
  await signIn(page, people.clare.handle);

  await expect(noGarden.heading()).toBeVisible();
  await expect(page.getByText(`You’re signed in as ${people.clare.name}`)).toBeVisible();
  await page.goto('/plants');
  await expect(noGarden.heading()).toBeVisible();

  await noGarden.setUp().click();

  await expect(page).toHaveURL('/setup/signed-in');
  await setup.garden().fill('Greenhouse');
  await setup.create().click();

  await expect(page).toHaveURL('/');
  await expect(page.getByRole('heading', { name: 'Greenhouse' })).toBeVisible();
  await expect(page.getByText('No plants yet')).toBeVisible();
  // Greenhouse is the account's only garden, so there is nothing to switch to.
  await expect(today.switchGarden()).toHaveCount(0);
});

test('an account in no garden can sign out', async ({ page, noGarden }) => {
  await signIn(page, people.clare.handle);
  await expect(noGarden.heading()).toBeVisible();

  await noGarden.signOut().click();

  await expect(page).toHaveURL('/signin');
  await page.goto('/');
  await expect(page).toHaveURL('/signin');
});

test('an empty garden name is refused under the field and the rest of the form is kept @passkey', async ({
  page,
  setup,
}) => {
  await setup.open();
  await setup.name().fill('Robin');
  await setup.timezone().selectOption('Asia/Tokyo');

  await setup.create().click();

  await expect(page).toHaveURL('/setup');
  await expect(page.getByText('Enter a name for your garden.')).toBeVisible();
  await expect(setup.name()).toHaveValue('Robin');
  await expect(setup.timezone()).toHaveValue('Asia/Tokyo');
});

test.describe('in a browser set to Tokyo', () => {
  test.use({ timezoneId: 'Asia/Tokyo' });

  test("the timezone select opens on the browser's own zone @js", async ({ setup }) => {
    await setup.open();

    await expect(setup.timezone()).toHaveValue('Asia/Tokyo');
  });
});

test('with no JavaScript the timezone select stays on Timezone and the page says setting up needs JavaScript @nojs', async ({
  page,
  setup,
}) => {
  await setup.open();

  await expect(setup.timezone()).toHaveValue('');
  await expect(setup.create()).toBeDisabled();
  await expect(page.getByText('Requires JavaScript and a browser with passkey support')).toBeVisible();
});
