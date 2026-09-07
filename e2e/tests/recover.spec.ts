import { aWorkingDevice, withDevice } from '../harness/authenticator';
import { devices, recoveryBatch } from '../harness/garden';
import { expect, test } from '../harness/test';

test('a recovery code registers a passkey without signing in, and that passkey then signs in @passkey', async ({
  page,
  passkeys,
  recover,
  recovery,
  signin,
}) => {
  await withDevice(page, aWorkingDevice, async () => {
    await signin.open();
    await page.getByRole('link', { name: 'use one' }).click();
    await expect(page).toHaveURL('/recover');
    await expect(page.getByRole('heading', { name: 'Recover an account' })).toBeVisible();

    await recover.code().fill(recoveryBatch.unused.toUpperCase());
    await recover.use().click();

    await expect(page.getByRole('heading', { name: 'Add this device' })).toBeVisible();
    await recover.registerPasskey().click();

    // Using a code never starts a session, so the browser lands on sign in
    // rather than in the garden.
    await expect(page).toHaveURL('/signin');
    await page.goto('/');
    await expect(page).toHaveURL('/signin');

    await signin.signIn().click();
    await expect(page).toHaveURL('/');

    await passkeys.open();
    await expect(passkeys.rows()).toHaveCount(3);
    await expect(passkeys.row(devices.phone)).toBeVisible();
    await expect(passkeys.row(devices.laptop)).toBeVisible();

    await recovery.open();
    await expect(page.getByText(`${recoveryBatch.left - 1} of ${recoveryBatch.size} left`)).toBeVisible();
  });
});

test('a code nobody made and a used code get the same page, with no form on it', async ({ page, recover }) => {
  await recover.open();
  await recover.code().fill('zzzz-zzzz-zzzz');
  await recover.use().click();

  await expect(page.getByRole('heading', { name: 'That code cannot be used' })).toBeVisible();
  await expect(page.getByText('Ask whoever runs the garden to send you a new invite link.')).toBeVisible();
  await expect(recover.code()).toHaveCount(0);
  await expect(recover.registerPasskey()).toHaveCount(0);

  await recover.tryAnother().click();
  await expect(page).toHaveURL('/recover');
  await recover.code().fill(recoveryBatch.used);
  await recover.use().click();

  await expect(page.getByRole('heading', { name: 'That code cannot be used' })).toBeVisible();
  await expect(page.getByText('Ask whoever runs the garden to send you a new invite link.')).toBeVisible();
  await expect(recover.code()).toHaveCount(0);
});

test('with no JavaScript a live code still opens Add this device, whose button is disabled @nojs', async ({
  page,
  recover,
}) => {
  await recover.open();
  await recover.code().fill(recoveryBatch.unused);
  await recover.use().click();

  await expect(page.getByRole('heading', { name: 'Add this device' })).toBeVisible();
  await expect(recover.registerPasskey()).toBeDisabled();
  await expect(
    page.getByText('Registering a passkey needs JavaScript and a browser that supports passkeys'),
  ).toBeVisible();
  await expect(page.getByRole('navigation')).toHaveCount(0);
});
