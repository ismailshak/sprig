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
  await expect(page.getByText('This is the only way you can sign in')).toBeVisible();
});
