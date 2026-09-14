import { people, plants as seeded } from '../harness/garden';
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
