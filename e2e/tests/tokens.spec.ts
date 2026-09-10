import { people, tokens as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('a token that has run out reads as expired and offers Remove where a live one offers Revoke', async ({
  tokens,
}) => {
  await tokens.open();

  await expect(tokens.row(seeded.spare)).toContainText(/Expired \w+/);
  await expect(tokens.row(seeded.spare).getByRole('button')).toHaveText('Remove');
  await expect(tokens.row(seeded.kitchen)).toContainText(/Expires \d+ \w+/);
  await expect(tokens.row(seeded.kitchen).getByRole('button')).toHaveText('Revoke');
  // The prefix is what tells the two display rows apart.
  await expect(tokens.row(seeded.spare)).toContainText(/sprg_\w+…/);
});

test('a new token is shown once, joins the list, and is gone on the next visit @swap', async ({ tokens, page }) => {
  await tokens.open();

  await tokens.name().fill('The greenhouse pi');
  await tokens.expiry().selectOption('7');
  await tokens.create().click();

  await expect(tokens.token()).toBeVisible();
  await expect(page.getByText(/It expires on \d+ \w+/)).toBeVisible();
  await expect(tokens.row('The greenhouse pi')).toBeVisible();

  await tokens.open();
  await expect(tokens.token()).toHaveCount(0);
  await expect(tokens.row('The greenhouse pi')).toBeVisible();
});

test('the new token gets a Copy button @js', async ({ tokens }) => {
  await tokens.open();

  await tokens.name().fill('The greenhouse pi');
  await tokens.expiry().selectOption('7');
  await tokens.create().click();

  await expect(tokens.copy()).toBeVisible();
});

test('without JavaScript the new token has no Copy button @nojs', async ({ tokens }) => {
  await tokens.open();

  await tokens.name().fill('The greenhouse pi');
  await tokens.expiry().selectOption('7');
  await tokens.create().click();

  await expect(tokens.token()).toBeVisible();
  await expect(tokens.copy()).toBeHidden();
});

test('a token with no name is refused and says so under the field @swap', async ({ tokens, page }) => {
  await tokens.open();

  await tokens.expiry().selectOption('90');
  await tokens.create().click();

  await expect(page.getByText(/^Enter a name\./)).toBeVisible();
  await expect(tokens.expiry()).toHaveValue('90');
  await expect(tokens.token()).toHaveCount(0);
});

test('a revoked token leaves the list and the rest stay @swap', async ({ tokens }) => {
  await tokens.open();

  await tokens.row(seeded.kitchen).getByRole('button').click();

  await expect(tokens.row(seeded.kitchen)).toHaveCount(0);
  await expect(tokens.row(seeded.spare)).toBeVisible();
});
