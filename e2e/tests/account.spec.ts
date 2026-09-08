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

  await expect(page.getByText('Enter a display name')).toBeVisible();
  await expect(account.name()).toHaveValue('');
});

test('a new handle is saved', async ({ account }) => {
  await account.open();

  await account.handle().fill('eleanor');
  await account.save().click();

  await expect(account.handle()).toHaveValue('eleanor');
});

test('a handle another account holds is refused and the page names it', async ({ account, page }) => {
  await account.open();

  await account.handle().fill(people.sam.handle);
  await account.save().click();

  await expect(page.getByText(`${people.sam.handle} is already taken`)).toBeVisible();
  await expect(account.handle()).toHaveValue(people.sam.handle);

  await account.open();
  await expect(account.handle()).toHaveValue(people.ellie.handle);
});

test('a handle typed with a capital and a space is saved in lower case with an underscore', async ({ account }) => {
  await account.open();

  await account.handle().fill('Emma Fletcher');
  await account.save().click();

  await expect(account.handle()).toHaveValue('emma_fletcher');
});
