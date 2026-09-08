import { people as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// The sentences People and Invite someone both use for what a role can do. The
// server writes them in one place, and these tests read them on both pages.
const roleWhat = {
  member: 'Members can log care, add and edit plants, and add photos.',
  sitter: 'Sitters can log care and view everything, but not add plants or photos.',
} as const;

test.beforeEach(async ({ page }) => {
  await signIn(page, seeded.ellie.handle);
});

test("a member's new role is what their row says after the save", async ({ people }) => {
  await people.open();

  await people.role(seeded.sam.name).selectOption('sitter');
  await people.save().click();

  await expect(people.role(seeded.sam.name)).toHaveValue('sitter');
  await expect(people.row(seeded.sam.name)).toContainText(roleWhat.sitter);
});

test('the owner is offered no role, no end date and no way to remove themselves', async ({ people }) => {
  await people.open();

  await expect(people.row(seeded.ellie.name)).toContainText('Owner');
  await expect(people.role(seeded.ellie.name)).toHaveCount(0);
  await expect(people.until(seeded.ellie.name)).toHaveCount(0);
  await expect(people.remove(seeded.ellie.name)).toHaveCount(0);
  await expect(people.reenrol(seeded.ellie.name)).toHaveCount(0);
});

test("a sitter's row says when their access ends and a permanent member's row has no date field", async ({
  people,
}) => {
  await people.open();

  await expect(people.row(seeded.jo.name)).toContainText('Sitter ·');
  await expect(people.row(seeded.jo.name)).toContainText(/until \d+ \w+/);
  await expect(people.until(seeded.jo.name)).toHaveCount(1);
  await expect(people.row(seeded.sam.name)).toContainText(roleWhat.member);
  await expect(people.until(seeded.sam.name)).toHaveCount(0);
});

test('a membership that has ended is offered no role until it is given a new date', async ({ people }) => {
  await people.open();

  await expect(people.row(seeded.clare.name)).toContainText(/Access ended \w+/);
  await expect(people.role(seeded.clare.name)).toHaveCount(0);

  await people.until(seeded.clare.name).fill('2099-06-01');
  await people.save().click();

  await expect(people.row(seeded.clare.name)).toContainText('Sitter · until 1 Jun');
  await expect(people.role(seeded.clare.name)).toHaveValue('sitter');
});

test('removing a member asks first, and keeping them leaves the row where it was', async ({ people, page }) => {
  await people.open();

  await people.remove(seeded.sam.name).click();

  await expect(page.getByText(`Remove ${seeded.sam.name}?`)).toBeVisible();
  await expect(page.getByText(/Their name stays on everything they’ve logged/)).toBeVisible();
  await people.cancelRemove().click();
  await expect(people.remove(seeded.sam.name)).toBeVisible();
});

test('a removed member leaves the list and the care they logged keeps their name', async ({ people, activity }) => {
  await people.open();
  await people.remove(seeded.sam.name).click();
  await people.confirmRemove().click();

  await expect(people.row(seeded.sam.name)).toHaveCount(0);
  await activity.open();
  await expect(activity.rows().filter({ hasText: seeded.sam.name }).first()).toBeVisible();
});

test('a re-enrolment link is shown once and is gone on the next visit', async ({ people }) => {
  await people.open();

  await people.reenrol(seeded.sam.name).click();

  await expect(people.link()).toContainText('/invite/');
  await people.open();
  await expect(people.link()).toHaveCount(0);
});

test('Pending invites is not shown once the last invite is revoked', async ({ people }) => {
  await people.open();

  await expect(people.invited()).toBeVisible();
  await people.revoke().click();

  await expect(people.invited()).toHaveCount(0);
});

test('pressing a role chip changes the sentence saying what that role can do', async ({ invite, page }) => {
  await invite.open();
  await expect(page.getByText(roleWhat.sitter)).toBeVisible();

  await invite.chip('Member').click();

  await expect(invite.chip('Member')).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByText(roleWhat.member)).toBeVisible();
  await expect(page.getByText(roleWhat.sitter)).toHaveCount(0);
});

test('an invite made with an end date is shown once and waits on People afterwards', async ({
  people,
  invite,
  page,
}) => {
  await people.open();
  await people.revoke().click();
  await people.inviteSomeone().click();

  await invite.chip('Member').click();
  await invite.until().fill('2099-06-01');
  await invite.create().click();

  await expect(invite.link()).toContainText('/invite/');
  await expect(page.getByText('Their access ends on 1 Jun')).toBeVisible();
  await invite.done().click();

  await expect(people.invited()).toBeVisible();
  await expect(page.getByText(/expires in \d+ days/)).toBeVisible();
});

// todayFor is today's date where the seeded owner is, in the format a date
// input reads. The owner's timezone is Europe/London, so the day is read
// there rather than where the test runs.
function todayFor(): string {
  return new Date().toLocaleDateString('en-CA', { timeZone: 'Europe/London' });
}

test('an end date of today is refused, because access would end before the link is opened', async ({
  invite,
  page,
}) => {
  await invite.open();

  await invite.until().fill(todayFor());
  await invite.create().click();

  await expect(page.getByText('Choose a date after today. Access ends at the start of that day.')).toBeVisible();
  await expect(invite.link()).toHaveCount(0);
  await expect(invite.until()).toHaveValue(todayFor());
});

// Without a hidden default submit button, Enter would fire the first role chip
// and reload the form with a different role pressed.
test('pressing Enter in the end date creates the link', async ({ invite, page }) => {
  await invite.open();

  await invite.until().fill('2099-06-01');
  await invite.until().press('Enter');

  await expect(invite.link()).toContainText('/invite/');
  await expect(page.getByText('Their access ends on 1 Jun')).toBeVisible();
});

test('the back link on Invite someone lands on People', async ({ invite, people, page }) => {
  await people.open();
  await people.inviteSomeone().click();

  await invite.back().click();

  await expect(page.getByRole('heading', { name: 'Members' })).toBeVisible();
});

test('the end date typed on the invite survives pressing a role chip', async ({ invite }) => {
  await invite.open();

  await invite.until().fill('2099-06-01');
  await invite.chip('Member').click();

  await expect(invite.until()).toHaveValue('2099-06-01');
});
