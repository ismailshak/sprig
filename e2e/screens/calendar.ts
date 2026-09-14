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
  // 2 due". In the current month the first day with care due is today when
  // anything is overdue or due today.
  firstDayWithCareDue(): Locator {
    return this.page.getByRole('link', { name: /\d+ due$/ }).first();
  }

  // Without JavaScript the click loads a new page, so this waits for it to load.
  async openDay(day: Locator): Promise<void> {
    await day.click();
    await this.page.waitForLoadState();
  }

  daySheet(): Locator {
    return this.page.getByRole('dialog');
  }

  // A plant's row in the day's sheet is a link whose name starts with the
  // plant's name.
  plantIn(plant: Plant): Locator {
    return this.daySheet().getByRole('link', { name: plant.name });
  }
}
