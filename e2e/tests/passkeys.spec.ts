import { aWorkingDevice, withDevice } from '../harness/authenticator';
import { devices, people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('the last passkey offers no Remove and the page says why', async ({ page, passkeys }) => {
  await passkeys.open();
  await expect(passkeys.row(devices.phone)).toBeVisible();

  await passkeys.removeOn(devices.laptop).click();

  await expect(passkeys.row(devices.laptop)).toHaveCount(0);
  await expect(passkeys.removeOn(devices.phone)).toHaveCount(0);
  await expect(page.getByText('Your only passkey can’t be removed')).toBeVisible();
});

test('adding a passkey puts the device on the list @passkey', async ({ page, passkeys }) => {
  await passkeys.open();
  await expect(passkeys.rows()).toHaveCount(2);

  await withDevice(page, aWorkingDevice, async () => {
    await passkeys.add().click();
    await expect(passkeys.rows()).toHaveCount(3);
  });

  // The devices already enrolled are untouched.
  await expect(passkeys.row(devices.phone)).toBeVisible();
  await expect(passkeys.row(devices.laptop)).toBeVisible();
  await expect(passkeys.refusal()).toHaveText('');
});

test('a device that already holds a passkey adds no second one @passkey', async ({ page, passkeys }) => {
  await passkeys.open();

  // One authenticator across both presses, because the claim is about a device
  // sprig has already seen. The registration names the account's credentials
  // as an exclude list, so the browser refuses the second prompt itself.
  await withDevice(page, aWorkingDevice, async () => {
    await passkeys.add().click();
    await expect(passkeys.rows()).toHaveCount(3);

    await passkeys.add().click();
    await expect(passkeys.refusal()).toHaveText('This device already has a passkey for sprig.');
  });

  await expect(passkeys.rows()).toHaveCount(3);
});

// The refusals below happen in the browser rather than at the server, because
// the registration asks for a stored passkey and a verified user and the
// browser will not run a ceremony a device cannot meet. Chrome reports both
// under the same error name as a cancelled prompt, so that a site cannot learn
// what somebody's devices can do by asking. One sentence covers all three
// cases, and both tests assert the whole of it.
const refused = 'No passkey was added. You may have cancelled, or this device can’t store passkeys.';

test('a device that will not store the passkey adds none, and the page says the browser does not say why @passkey', async ({
  page,
  passkeys,
}) => {
  await passkeys.open();

  await withDevice(page, { stores: false, verifies: true, unlocked: true }, async () => {
    await passkeys.add().click();
    await expect(passkeys.refusal()).toHaveText(refused);
  });

  await expect(passkeys.rows()).toHaveCount(2);
});

test('a device that does not check who is using it adds none, and the page says the browser does not say why @passkey', async ({
  page,
  passkeys,
}) => {
  await passkeys.open();

  await withDevice(page, { stores: true, verifies: true, unlocked: false }, async () => {
    await passkeys.add().click();
    await expect(passkeys.refusal()).toHaveText(refused);
  });

  await expect(passkeys.rows()).toHaveCount(2);
});

test('a security key with no PIN adds none, and the page says the browser does not say why @passkey', async ({
  page,
  passkeys,
}) => {
  await passkeys.open();

  await withDevice(page, { stores: true, verifies: false, unlocked: false }, async () => {
    await passkeys.add().click();
    await expect(passkeys.refusal()).toHaveText(refused);
  });

  await expect(passkeys.rows()).toHaveCount(2);
});

test('Add passkey can be pressed again after a device is refused @passkey', async ({ page, passkeys }) => {
  await passkeys.open();

  await withDevice(page, { stores: false, verifies: true, unlocked: true }, async () => {
    await passkeys.add().click();
    await expect(passkeys.refusal()).not.toHaveText('');
  });

  await withDevice(page, aWorkingDevice, async () => {
    await passkeys.add().click();
    await expect(passkeys.rows()).toHaveCount(3);
  });
});

test('with no JavaScript Add passkey is disabled and the page says it needs JavaScript @nojs', async ({
  page,
  passkeys,
}) => {
  await passkeys.open();

  await expect(passkeys.add()).toBeDisabled();
  await expect(page.getByText('Requires JavaScript and a browser with passkey support')).toBeVisible();
});
