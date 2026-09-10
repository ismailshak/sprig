import { people, plants } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page, today }) => {
  await signIn(page, people.ellie.handle);
  await today.open();
});

test('clicking a care row opens the log sheet for that care', async ({ today, sheet }) => {
  await today.openSheet(plants.nigel, 'water');

  await expect(sheet.dialog()).toHaveAccessibleName('Log care for Nigel');
  await expect(sheet.dialog().getByRole('link', { name: /Nigel/ })).toBeVisible();
  await expect(sheet.chip('Water')).toBeChecked();
  await expect(sheet.chip('Feed')).not.toBeChecked();
  await expect(sheet.chip('Just now')).toBeChecked();
  await expect(sheet.dialog().getByRole('button', { name: 'Log watering' })).toBeVisible();
});

test('the sheet has no care type choice for a plant with one care', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');

  await expect(sheet.dialog()).toBeVisible();
  await expect(sheet.dialog().getByText('Care', { exact: true })).toHaveCount(0);
});

test("logging a care from the sheet removes the plant from today's list @swap", async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');
  await sheet.submit('Log watering');
  await expect(sheet.dialog()).toHaveCount(0);

  await today.open();
  await expect(today.careRow(plants.doris, 'water')).toHaveCount(0);
});

test('the care button on a row logs the care with the current time @swap', async ({ today }) => {
  await today.careButton(plants.doris, 'water').click();
  // With JavaScript the row stays in its logged state, so a locator for any
  // button would match its Undo.
  await expect(today.careRow(plants.doris, 'water').getByRole('button', { name: 'Water' })).toHaveCount(0);

  await today.open();
  await expect(today.careRow(plants.doris, 'water')).toHaveCount(0);
});

test('a skip makes the care due again after the chosen number of days @swap', async ({ today, sheet }) => {
  await today.openSheet(plants.nigel, 'water');
  await sheet.chip('Skipped').check();
  await sheet.chip('1 day').check();
  await sheet.submit('Log skip');
  await expect(sheet.dialog()).toHaveCount(0);

  await today.open();
  const row = today.careRow(plants.nigel, 'water');
  await expect(today.section('Coming up').locator(row)).toBeVisible();
  await expect(row).toContainText('Water tomorrow');
});

test("choosing another care type shows that type's usual interval", async ({ today, sheet }) => {
  await today.openSheet(plants.nigel, 'water');
  await sheet.chip('Skipped').check();
  await expect(sheet.chip('4 days (usual)')).toBeVisible();

  await sheet.chip('Feed').check();

  await expect(sheet.chip('4 days (usual)')).toBeHidden();
  await expect(sheet.chip('21 days (usual)')).toBeVisible();
  await expect(sheet.chip('Skipped')).toBeChecked();
  await expect(sheet.dialog().getByRole('button', { name: 'Log skip' })).toBeVisible();
});

test('a time later than now is refused @swap', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');
  await sheet.chip('Earlier today').check();
  await sheet.time().fill('23:59');
  await sheet.submit('Log watering');

  await expect(sheet.dialog().getByText('That time is in the future.')).toBeVisible();
  await expect(sheet.chip('Earlier today')).toBeChecked();
});

test('the sheet takes focus when it opens and Escape puts it back on the row @js', async ({ page, today, sheet }) => {
  await today.rowLink(plants.nigel, 'water').focus();
  await page.keyboard.press('Enter');

  await expect(sheet.dialog()).toBeFocused();

  await page.keyboard.press('Escape');

  await expect(sheet.dialog()).toBeHidden();
  await expect(today.rowLink(plants.nigel, 'water')).toBeFocused();
});

// Chrome wraps Tab from the last control to the first. WebKit leaves the
// document for one press and comes back in at the sheet itself on the next.
// Neither reaches the page behind the sheet.
test('Tab from the last control in the sheet never reaches the page behind it @js', async ({ page, today, sheet }) => {
  await today.rowLink(plants.nigel, 'water').focus();
  await page.keyboard.press('Enter');
  await sheet.dialog().getByRole('button', { name: 'Cancel' }).focus();

  await page.keyboard.press('Tab');
  await expect(page.getByRole('main').locator(':focus')).toHaveCount(0);

  await page.keyboard.press('Tab');
  await expect(page.locator('dialog:focus, dialog :focus')).toHaveCount(1);
});

test('logging from the sheet is announced with what is left and where Undo is @js', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');
  await sheet.submit('Log watering');

  await expect(today.announcement()).toContainText('You watered Doris.');
  await expect(today.announcement()).toContainText('Undo from the row now, or from Activity later.');
});

// The wait is the 4s window plus the 320ms collapse after it, rounded up. A
// row still on the page afterwards was held by the focus.
test('the undo window pauses while Undo has keyboard focus and resumes when focus leaves @js', async ({
  page,
  today,
}) => {
  await today.careButton(plants.doris, 'water').focus();
  await page.keyboard.press('Enter');
  await expect(today.undoButton(plants.doris, 'water')).toBeFocused();

  // 5s is the 4s window plus the 320ms collapse, so the row would be gone by
  // now if focus had not paused it.
  await page.waitForTimeout(5000);
  await expect(today.undoButton(plants.doris, 'water')).toBeVisible();

  // Focus arrived before the window started, so the whole 4s is left once
  // focus leaves. Halfway through it the row is still there.
  await page.keyboard.press('Tab');
  await page.waitForTimeout(2000);
  await expect(today.undoButton(plants.doris, 'water')).toBeVisible();

  await expect(today.careRow(plants.doris, 'water')).toHaveCount(0);
});

test('above 900px the sheet still traps focus and gives it back on Escape @wide', async ({ page, today, sheet }) => {
  await today.rowLink(plants.nigel, 'water').focus();
  await page.keyboard.press('Enter');

  await expect(sheet.dialog()).toBeFocused();

  await sheet.dialog().getByRole('button', { name: 'Cancel' }).focus();
  await page.keyboard.press('Tab');
  await expect(page.getByRole('main').locator(':focus')).toHaveCount(0);

  await page.keyboard.press('Escape');

  await expect(sheet.dialog()).toBeHidden();
  await expect(today.rowLink(plants.nigel, 'water')).toBeFocused();
});

test('cancelling the sheet leaves the row unchanged', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');
  await sheet.cancel();

  await expect(sheet.dialog()).toBeHidden();
  await expect(today.careRow(plants.doris, 'water')).toBeVisible();
});

test('a logged row reads Watered just now without a reload @js', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');
  await sheet.submit('Log watering');

  await expect(today.careRow(plants.doris, 'water')).toContainText('Watered just now');
  await expect(sheet.dialog()).toHaveCount(0);
});

test('a logged row can be undone inside its grace window @js', async ({ today }) => {
  await today.careButton(plants.doris, 'water').click();
  await expect(today.careRow(plants.doris, 'water')).toContainText('Watered just now');

  await today.undoButton(plants.doris, 'water').click();

  const row = today.careRow(plants.doris, 'water');
  await expect(row).toHaveText(/^\s*Doris\s*Bedroom\s*Water\s*$/);
  await expect(row.getByRole('button', { name: 'Water' })).toBeVisible();
});

// The 15s wait covers the 4s grace window and the collapse after it, inside
// Playwright's 30s test timeout.
test('a logged row disappears from the list when its grace window closes @js', async ({ today }) => {
  await today.careButton(plants.doris, 'water').click();
  await expect(today.undoButton(plants.doris, 'water')).toBeVisible();

  await expect(today.careRow(plants.doris, 'water')).toHaveCount(0, { timeout: 15_000 });
});

// The seed holds months of history. Five is the server's limit on the feed.
test('the feed shows the five newest events', async ({ today }) => {
  await expect(today.feedLines()).toHaveCount(5);
});

test('a care logged from the sheet appears at the top of the feed @js', async ({ today, sheet }) => {
  await today.openSheet(plants.doris, 'water');
  await sheet.submit('Log watering');

  await expect(today.feedLines().first()).toContainText('You watered Doris');
});

// The reload after the post removes the row from the day. The only Undo is in
// the feed, on the care just logged, because every seeded event is days old and
// outside the undo window.
test('a logged care can be undone from the feed without JavaScript @nojs', async ({ today }) => {
  await today.careButton(plants.doris, 'water').click();
  await expect(today.careRow(plants.doris, 'water')).toHaveCount(0);

  await expect(today.feedUndo()).toHaveCount(1);
  await expect(today.feedLines().first().getByRole('button', { name: 'Undo' })).toBeVisible();

  await today.feedUndo().click();

  await expect(today.careRow(plants.doris, 'water')).toBeVisible();
});
