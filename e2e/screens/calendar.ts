import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

export class CalendarScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/activity/calendar');
  }

  // The month's name is read from inside #calendar because a day's sheet has an
  // h2 too.
  month(): Locator {
    return this.page.locator('#calendar').getByRole('heading', { level: 2 });
  }

  previousMonth(): Locator {
    return this.page.getByRole('link', { name: 'Previous month' });
  }

  nextMonth(): Locator {
    return this.page.getByRole('link', { name: 'Next month' });
  }

  // The tab bar has a Today link too, so this one is looked for inside main.
  today(): Locator {
    return this.page.getByRole('main').getByRole('link', { name: 'Today', exact: true });
  }

  // A day with care on it is a link named like "Monday 14 September, 1 logged,
  // 2 due, Jo sitting". In the current month the first day with care due is
  // today when anything is overdue or due today.
  firstDayWithCareDue(): Locator {
    return this.page.getByRole('link', { name: /\d+ due(,|$)/ }).first();
  }

  // With JavaScript the click swaps the day's sheet in. Without it the click
  // loads the page with the sheet open.
  async openDay(day: Locator): Promise<void> {
    await day.click();
    await this.daySheet().waitFor();
  }

  daySheet(): Locator {
    return this.page.getByRole('dialog');
  }

  // A plant's row in the day's sheet is a link whose name starts with the
  // plant's name.
  plantIn(plant: Plant): Locator {
    return this.daySheet().getByRole('link', { name: plant.name });
  }

  // Each day with the note is a link whose accessible name has the note's text
  // after the date, "Monday 14 September, Ellie away, 2 due".
  daysWithNote(text: string): Locator {
    return this.page.getByRole('link', { name: text });
  }

  // A row under Notes in the day's sheet. It is a link to the Edit note sheet
  // for a member who may edit notes and plain text for anyone else.
  noteIn(text: string): Locator {
    return this.daySheet().getByRole('listitem').filter({ hasText: text });
  }

  addNote(): Locator {
    return this.daySheet().getByRole('link', { name: 'Add note' });
  }

  // With JavaScript the click swaps the note's sheet in place of the day's.
  // Without it the click loads the calendar with the note's sheet open.
  async openNote(row: Locator): Promise<void> {
    await row.click();
    await this.noteSheet().waitFor();
  }

  // The note's sheet is the dialog holding the Note field.
  noteSheet(): Locator {
    return this.page.getByRole('dialog').filter({ has: this.page.getByLabel('Note', { exact: true }) });
  }

  noteText(): Locator {
    return this.noteSheet().getByLabel('Note', { exact: true });
  }

  noteFirstDay(): Locator {
    return this.noteSheet().getByLabel('First day');
  }

  noteLastDay(): Locator {
    return this.noteSheet().getByLabel('Last day');
  }

  // The primary button reads "Add note" on a new note and "Save changes" on
  // one being edited.
  noteButton(label: string): Locator {
    return this.noteSheet().getByRole('button', { name: label, exact: true });
  }

  // A saved note closes the sheet, with JavaScript by a swap and without it by
  // the redirect to the month.
  async saveNote(label: string): Promise<void> {
    await this.noteButton(label).click();
    await this.noteSheet().waitFor({ state: 'detached' });
  }

  async deleteNote(): Promise<void> {
    await this.noteButton('Delete').click();
    await this.noteSheet().waitFor({ state: 'detached' });
  }

  // A row under Sitting in the day's sheet. The signed-in person's own row is
  // named "You". A care row can contain the same name, as in "Jo watered", but
  // a care row is a link and a Sitting row is not.
  sitterIn(name: string): Locator {
    return this.daySheet()
      .getByRole('listitem')
      .filter({ hasText: name, hasNot: this.page.getByRole('link') });
  }
}
