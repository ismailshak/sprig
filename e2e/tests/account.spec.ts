import { people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('a new display name and timezone are saved', async ({ account, more }) => {
  await more.open();
  await more.row('Account').click();

  await account.name().fill('Eleanor');
  await account.timezone().selectOption('Asia/Tokyo');
  await account.save().click();

  await expect(account.name()).toHaveValue('Eleanor');
  await expect(account.timezone()).toHaveValue('Asia/Tokyo');
});

test('an empty display name is refused and the page says what is missing', async ({ account, page }) => {
  await account.open();

  await account.name().fill('');
  await account.save().click();

  await expect(page.getByText('Give a display name')).toBeVisible();
  await expect(account.name()).toHaveValue('');
});
