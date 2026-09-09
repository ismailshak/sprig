import { gardens, people } from '../harness/garden';
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

test('the only owner of a garden is told to delete it before closing the account', async ({
  account,
  closeAccount,
  page,
}) => {
  await account.open();
  await account.closeAccount().click();

  await expect(page.getByText(`You’re the only owner of ${gardens.home.name}.`)).toBeVisible();
  await expect(closeAccount.confirm()).toHaveCount(0);
});

test('a closed account cannot sign in and its name stays on Activity', async ({
  account,
  closeAccount,
  activity,
  page,
}) => {
  await signIn(page, people.sam.handle);
  await account.open();
  await account.closeAccount().click();

  await closeAccount.handle().fill(people.sam.handle);
  await closeAccount.confirm().click();

  await expect(page).toHaveURL('/signin');
  await page.goto('/dev/signin');
  await expect(page.getByRole('button', { name: `(${people.sam.handle})` })).toHaveCount(0);

  await signIn(page, people.ellie.handle);
  await activity.open();
  await expect(activity.rows().filter({ hasText: people.sam.name }).first()).toBeVisible();
});

test('closing an account with the wrong handle typed is refused', async ({ account, closeAccount, page }) => {
  await signIn(page, people.sam.handle);
  await closeAccount.open();

  await closeAccount.handle().fill(people.ellie.handle);
  await closeAccount.confirm().click();

  await expect(page.getByText('That isn’t your handle.')).toBeVisible();
  await expect(closeAccount.handle()).toHaveValue(people.ellie.handle);
  await account.open();
  await expect(account.handle()).toHaveValue(people.sam.handle);
});
