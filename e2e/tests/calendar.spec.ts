import { notes, people, plants as seeded } from '../harness/garden';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

test.beforeEach(async ({ page }) => {
  await signIn(page, people.ellie.handle);
});

test('the calendar link on Activity opens the calendar', async ({ page, activity, calendar }) => {
  await activity.open();

  await activity.calendar().click();

  await expect(page).toHaveURL('/activity/calendar');
  await expect(calendar.month()).toBeVisible();
});

test('Coming up on Today links to the calendar', async ({ page, today }) => {
  await today.open();

  await today.calendar().click();

  await expect(page).toHaveURL('/activity/calendar');
});

test('Next and then Previous return to the month the calendar opened on @swap', async ({ page, calendar }) => {
  await calendar.open();
  const opened = (await calendar.month().textContent()) ?? '';

  await calendar.nextMonth().click();

  await expect(page).toHaveURL(/\/activity\/calendar\?month=\d{4}-\d{2}$/);
  await expect(calendar.month()).not.toHaveText(opened);

  await calendar.previousMonth().click();

  await expect(calendar.month()).toHaveText(opened);
});

test('Today after Next returns to the month the calendar opened on @swap', async ({ page, calendar }) => {
  await calendar.open();
  const opened = (await calendar.month().textContent()) ?? '';

  await calendar.nextMonth().click();
  await expect(calendar.month()).not.toHaveText(opened);

  await calendar.today().click();

  await expect(page).toHaveURL('/activity/calendar');
  await expect(calendar.month()).toHaveText(opened);
});

test('Back after Next shows the month the calendar opened on', async ({ page, calendar }) => {
  await calendar.open();
  const opened = (await calendar.month().textContent()) ?? '';

  await calendar.nextMonth().click();
  await expect(calendar.month()).not.toHaveText(opened);

  await page.goBack();

  await expect(calendar.month()).toHaveText(opened);
});

// The seed has Big Fella's watering 2 days overdue. The calendar lists overdue
// care on today.
test("overdue care in today's sheet links to the plant @swap", async ({ page, calendar }) => {
  await calendar.open();

  await calendar.openDay(calendar.firstDayWithCareDue());

  await expect(calendar.plantIn(seeded.bigFella)).toContainText('2 days late');

  await calendar.plantIn(seeded.bigFella).click();

  await expect(page).toHaveURL(`/plants/${seeded.bigFella.id}`);
});

// The seed made Jo's membership 20 days ago and ends it in 8 days.
test("an owner's day sheet names the sitter whose access covers the day @swap", async ({ calendar }) => {
  await calendar.open();

  await calendar.openDay(calendar.firstDayWithCareDue());

  await expect(calendar.sitterIn(people.jo.name)).toContainText('until');
});

test("a sitter's day sheet says when their own access ends @swap", async ({ page, calendar }) => {
  await signIn(page, people.jo.handle);
  await calendar.open();

  await calendar.openDay(calendar.firstDayWithCareDue());

  await expect(calendar.sitterIn('You')).toContainText('until');
});

// The seed's note starts today, the first day with care due.
test("a member opens a note from the day's sheet", async ({ calendar }) => {
  await calendar.open();

  await calendar.openDay(calendar.firstDayWithCareDue());
  await expect(calendar.noteIn(notes.away.text)).toBeVisible();

  await calendar.openNote(calendar.noteIn(notes.away.text).getByRole('link'));

  await expect(calendar.noteText()).toHaveValue(notes.away.text);
  await expect(calendar.noteButton('Delete')).toBeVisible();
});

test("a sitter sees a note on the day's sheet as plain text with no Add note link", async ({ page, calendar }) => {
  await signIn(page, people.jo.handle);
  await calendar.open();

  await calendar.openDay(calendar.firstDayWithCareDue());

  await expect(calendar.noteIn(notes.away.text)).toBeVisible();
  await expect(calendar.noteIn(notes.away.text).getByRole('link')).toHaveCount(0);
  await expect(calendar.addNote()).toHaveCount(0);
});

test("a note added from a day's sheet shows on that day @swap", async ({ calendar }) => {
  await calendar.open();
  const day = calendar.firstDayWithCareDue();
  const date = ((await day.getAttribute('aria-label')) ?? '').split(',')[0];

  await calendar.openDay(day);
  await calendar.openNote(calendar.addNote());
  await calendar.noteText().fill('Heatwave');
  await calendar.saveNote('Add note');

  await expect(calendar.daysWithNote('Heatwave')).toHaveAccessibleName(new RegExp(`^${date}, `));
});

test('after a note is saved, focus is back on the day it was opened from @js', async ({ calendar }) => {
  await calendar.open();

  await calendar.openDay(calendar.firstDayWithCareDue());
  await calendar.openNote(calendar.addNote());
  await calendar.noteText().fill('Heatwave');
  await calendar.saveNote('Add note');

  await expect(calendar.daysWithNote('Heatwave')).toBeFocused();
});

test('a last day before the first is refused with the reason @swap', async ({ calendar }) => {
  await calendar.open();

  await calendar.openDay(calendar.firstDayWithCareDue());
  await calendar.openNote(calendar.addNote());
  await calendar.noteText().fill('Party');
  // The first day is the day the sheet was opened from. The day before it is
  // computed in UTC so a clock change cannot move it.
  const first = new Date(`${await calendar.noteFirstDay().inputValue()}T00:00:00Z`);
  first.setUTCDate(first.getUTCDate() - 1);
  await calendar.noteLastDay().fill(first.toISOString().slice(0, 10));
  await calendar.noteButton('Add note').click();

  await expect(calendar.noteSheet()).toContainText('Choose a last day on or after the first.');
  await expect(calendar.noteText()).toHaveValue('Party');
});

test('an edited note shows its new text on its days @swap', async ({ calendar }) => {
  await calendar.open();

  await calendar.openDay(calendar.firstDayWithCareDue());
  await calendar.openNote(calendar.noteIn(notes.away.text).getByRole('link'));
  await calendar.noteText().fill('Ellie in Lisbon');
  await calendar.saveNote('Save changes');

  await expect(calendar.daysWithNote('Ellie in Lisbon').first()).toBeVisible();
  await expect(calendar.daysWithNote(notes.away.text)).toHaveCount(0);
});

test("a deleted note is no longer shown on its days or in the day's sheet @swap", async ({ calendar }) => {
  await calendar.open();

  await calendar.openDay(calendar.firstDayWithCareDue());
  await calendar.openNote(calendar.noteIn(notes.away.text).getByRole('link'));
  await calendar.deleteNote();

  await expect(calendar.daysWithNote(notes.away.text)).toHaveCount(0);

  await calendar.openDay(calendar.firstDayWithCareDue());
  await expect(calendar.noteIn(notes.away.text)).toHaveCount(0);
});
