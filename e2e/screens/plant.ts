import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

export class PlantScreen {
  constructor(private readonly page: Page) {}

  async open(plant: Plant): Promise<void> {
    await this.page.goto(`/plants/${plant.id}`);
  }

  heading(): Locator {
    return this.page.getByRole('heading', { level: 1 });
  }

  // The profile picture at the top of the page. A plant with no picture has an
  // icon in its place, so there is no image to match.
  picture(): Locator {
    return this.page.getByRole('img', { name: /^Picture of / });
  }

  section(title: 'Schedule' | 'Details' | 'Photos' | 'Recent activity'): Locator {
    return this.page.getByRole('region', { name: title });
  }

  // Pressing the picture goes to that photo's own page.
  async openPicture(): Promise<void> {
    await this.picture().click();
    await this.page.waitForLoadState();
  }

  // The Add tile at the head of the Photos strip, for a member who may add
  // photos.
  addPhoto(): Locator {
    return this.section('Photos').getByRole('link', { name: 'Add', exact: true });
  }

  // The photos in the strip, newest first. Each is a link to its own page,
  // named by its image.
  stripPhotos(): Locator {
    return this.section('Photos').getByRole('link', { name: /^Photo of / });
  }

  // A schedule row is found by its care type, the one part that does not change
  // when the plant is watered.
  scheduleRow(care: string): Locator {
    return this.section('Schedule').getByRole('listitem').filter({ hasText: care });
  }

  // The whole row is the control, so opening the editor is a request either
  // way. With JavaScript it is an htmx swap, without it a navigation.
  async editSchedule(care: string): Promise<void> {
    await this.scheduleRow(care).getByRole('link').click();
    await this.page.waitForLoadState();
  }

  // Every control's label includes its care type. The add form's rows use the
  // same labels.
  shape(care: string): Locator {
    return this.page.getByLabel(`When ${care} happens`);
  }

  every(care: string): Locator {
    return this.page.getByLabel(`How often ${care} repeats`);
  }

  unit(care: string): Locator {
    return this.page.getByLabel(`${care} interval`);
  }

  date(care: string, part: 'day' | 'month' | 'year'): Locator {
    return this.page.getByLabel(`${care} ${part}`);
  }

  async saveSchedule(care: string): Promise<void> {
    await this.scheduleRow(care).getByRole('button', { name: 'Save' }).click();
    await this.page.waitForLoadState();
  }

  async cancelSchedule(care: string): Promise<void> {
    await this.scheduleRow(care).getByRole('link', { name: 'Cancel' }).click();
    await this.page.waitForLoadState();
  }

  // Remove asks for confirmation. The first click replaces the editor's buttons
  // with the question and the second confirms.
  async removeSchedule(care: string): Promise<void> {
    await this.askToRemoveSchedule(care);
    await this.scheduleRow(care).getByRole('button', { name: 'Remove' }).click();
    await this.page.waitForLoadState();
  }

  // The editor's Cancel is a link and the confirmation's is a button, so this
  // waits for the swap. There is no misclick to guard against here, because the
  // editor's Remove is a link and the confirmation's is a button too.
  async askToRemoveSchedule(care: string): Promise<void> {
    await this.scheduleRow(care).getByRole('link', { name: 'Remove' }).click();
    await this.scheduleRow(care).getByRole('button', { name: 'Cancel' }).waitFor();
  }

  async cancelRemoveSchedule(care: string): Promise<void> {
    await this.scheduleRow(care).getByRole('button', { name: 'Cancel' }).click();
    await this.page.waitForLoadState();
  }

  detailLabels(): Locator {
    return this.section('Details').getByRole('term');
  }

  detailValues(): Locator {
    return this.section('Details').getByRole('definition');
  }

  recentLines(): Locator {
    return this.section('Recent activity').getByRole('listitem');
  }

  // The All activity link under Recent activity. It opens the Activity page
  // filtered to this plant.
  allActivity(): Locator {
    return this.section('Recent activity').getByRole('link');
  }

  async edit(): Promise<void> {
    await this.page.getByRole('link', { name: 'Edit plant' }).click();
    await this.page.waitForLoadState();
  }

  // Archive asks for confirmation. The first click replaces the buttons with
  // the question and the second confirms.
  async archive(): Promise<void> {
    await this.askToArchive();
    await this.foot().getByRole('button', { name: 'Archive' }).click();
    await this.page.waitForLoadState();
  }

  // The confirmation's Archive button has the same accessible name as the
  // button clicked here. Waiting for the confirmation's Cancel first stops the
  // caller's next click pressing this button again before the swap lands.
  async askToArchive(): Promise<void> {
    await this.foot().getByRole('button', { name: 'Archive' }).click();
    await this.foot().getByRole('button', { name: 'Cancel' }).waitFor();
  }

  async cancelArchive(): Promise<void> {
    await this.foot().getByRole('button', { name: 'Cancel' }).click();
    await this.page.waitForLoadState();
  }

  // The confirmation and the buttons share this id. The server names it as the
  // swap target.
  foot(): Locator {
    return this.page.locator('#plant-foot');
  }

  // Restore posts and redirects, with no confirmation step, so the click is a
  // full navigation.
  async restore(): Promise<void> {
    await this.page.getByRole('button', { name: 'Restore' }).click();
    await this.page.waitForLoadState();
  }

  // Log care is a link without JavaScript, so the click is a navigation.
  async logCare(): Promise<void> {
    await this.page.getByRole('link', { name: 'Log care' }).click();
    await this.page.waitForLoadState();
  }
}
