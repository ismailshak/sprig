import { people, recoveryBatch } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

// The seeded owner already has codes, so neither the More row nor the Account
// row shows a note.
test('the recovery codes page says how much of the batch is left and that a new set replaces it', async ({
  account,
  more,
  page,
  recovery,
}) => {
  await more.open();
  await expect(more.row('Account')).not.toContainText('No recovery codes');

  await more.row('Account').click();
  await expect(account.recoveryCodes()).not.toContainText('None yet');

  await account.recoveryCodes().click();
  await expect(page.getByRole('heading', { name: 'Recovery codes' })).toBeVisible();
  await expect(page.getByText(`${recoveryBatch.left} of ${recoveryBatch.size} left`)).toBeVisible();
  await expect(page.getByText('Creating a new set destroys this one')).toBeVisible();
  await expect(recovery.create()).toHaveText('Create new codes');
});

test('the back link on recovery codes lands on the account page', async ({ account, recovery }) => {
  await recovery.open();

  await recovery.back().click();

  await expect(account.handle()).toHaveValue(people.ellie.handle);
});

test('creating codes shows ten once, and the page then reads 10 of 10 left', async ({ account, page, recovery }) => {
  await recovery.open();
  await recovery.create().click();

  await expect(recovery.codes()).toHaveCount(recoveryBatch.size);
  await expect(page.getByText('This is the only time they are shown')).toBeVisible();
  await expect(recovery.create()).toHaveCount(0);

  await recovery.done().click();
  await expect(account.handle()).toHaveValue(people.ellie.handle);

  await account.recoveryCodes().click();
  await expect(page.getByText('This is the only time they are shown')).toHaveCount(0);
  await expect(page.getByText(`${recoveryBatch.size} of ${recoveryBatch.size} left`)).toBeVisible();
  await expect(recovery.create()).toHaveText('Create new codes');
});
