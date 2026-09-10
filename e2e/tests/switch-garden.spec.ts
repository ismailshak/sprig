import { gardens, invites, people } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test('switching garden changes Today and the role on More @swap', async ({ page, today, more }) => {
  await signIn(page, people.sam.handle);
  await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();
  await expect(page.getByText(`${people.ellie.name}’s garden`)).toBeVisible();

  await today.switchGarden().click();

  await expect(today.gardenSheet()).toBeVisible();
  await expect(today.gardenRow(gardens.home.name)).toContainText(`${people.ellie.name}’s garden`);
  await expect(today.gardenRow(gardens.home.name)).toContainText('Member');
  await expect(today.switchTo(gardens.home.name)).toHaveCount(0);
  await expect(today.gardenRow(gardens.upstairs.name)).toContainText(`${people.robin.name}’s garden`);
  await expect(today.gardenRow(gardens.upstairs.name)).toContainText('Sitter');

  await today.switchTo(gardens.upstairs.name).click();

  await expect(page).toHaveURL('/');
  await expect(page.getByRole('heading', { name: gardens.upstairs.name })).toBeVisible();
  await expect(page.getByText(`${people.robin.name}’s garden`)).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: gardens.upstairs.name })).toBeVisible();

  // Sam is a sitter in Upstairs, so More is a sitter's index there.
  await more.open();
  await expect(more.row('Tokens')).toHaveCount(0);

  await today.open();
  await today.switchGarden().click();
  await expect(today.switchTo(gardens.upstairs.name)).toHaveCount(0);
  await expect(today.switchTo(gardens.home.name)).toBeVisible();
});

test('one garden has no switch icon and no owner line', async ({ page, today }) => {
  await signIn(page, people.ellie.handle);

  await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();
  await expect(today.switchGarden()).toHaveCount(0);
  await expect(page.getByText(/’s garden$/)).toHaveCount(0);
});

test('deleting the garden you own lands you on Today in the garden you sit for @swap', async ({
  page,
  accept,
  deleteGarden,
  invited,
  today,
}) => {
  // Robin owns Upstairs. Joining Home from the seeded invite makes Robin a
  // sitter there and moves the session onto Home.
  await signIn(page, people.robin.handle);
  await invited.open(invites.sitter.token);
  await accept.join(gardens.home.name).click();
  await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();

  await today.switchGarden().click();
  await today.switchTo(gardens.upstairs.name).click();
  await expect(page.getByRole('heading', { name: gardens.upstairs.name })).toBeVisible();

  await deleteGarden.open();
  await deleteGarden.name().fill(gardens.upstairs.name);
  await deleteGarden.confirm().click();

  await expect(page).toHaveURL('/');
  await expect(page.getByRole('heading', { name: gardens.home.name })).toBeVisible();
  await expect(page.getByText(`${people.ellie.name}’s garden`)).toBeVisible();
  await expect(today.switchGarden()).toHaveCount(0);
});
