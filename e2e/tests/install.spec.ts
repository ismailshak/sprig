import { people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test('Install sprig shows the steps for the platform that was chosen', async ({ page, install, more }) => {
  await signIn(page, people.ellie.handle);
  await more.open();

  await more.install().click();
  await expect(install.steps().first()).toContainText('Safari');

  await install.platform('Android').click();

  await expect(install.steps().first()).toContainText('Chrome');
  await expect(install.platform('Android')).toHaveAttribute('aria-pressed', 'true');
});
