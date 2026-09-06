import { attach, aWorkingDevice } from '../harness/authenticator';
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

test('an empty garden name is refused under the field and the rest of the form is kept @passkey', async ({
  page,
  setup,
}) => {
  await setup.open();
  await setup.name().fill('Robin');
  await setup.timezone().selectOption('Asia/Tokyo');

  await setup.create().click();

  await expect(page).toHaveURL('/setup');
  await expect(page.getByText('Give the garden a name.')).toBeVisible();
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
  await expect(page.getByText('Setting up needs JavaScript and a browser that supports passkeys')).toBeVisible();
});
