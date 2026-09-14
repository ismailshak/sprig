import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';
import { photoPage } from './photos';

// The URL of a plant's own page. Matching the id keeps it from matching
// /plants/new.
export const plantPage = /\/plants\/[0-9a-f-]{36}$/;

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
    await this.page.waitForURL(photoPage);
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
    await this.scheduleRow(care).getByRole('button', { name: 'Save' }).waitFor();
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
    const save = this.scheduleRow(care).getByRole('button', { name: 'Save' });
    await save.click();
    await save.waitFor({ state: 'detached' });
  }

  async cancelSchedule(care: string): Promise<void> {
    await this.scheduleRow(care).getByRole('link', { name: 'Cancel' }).click();
    await this.scheduleRow(care).getByRole('button', { name: 'Save' }).waitFor({ state: 'detached' });
  }

  // Remove asks for confirmation. The first click replaces the editor's buttons
  // with the question and the second confirms.
  async removeSchedule(care: string): Promise<void> {
    await this.askToRemoveSchedule(care);
    const remove = this.scheduleRow(care).getByRole('button', { name: 'Remove' });
    await remove.click();
    await remove.waitFor({ state: 'detached' });
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
    await this.scheduleRow(care).getByRole('link', { name: 'Remove' }).waitFor();
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
    await this.page.waitForURL(/\/edit$/);
  }

  // Archive asks for confirmation. The first click replaces the buttons with
  // the question and the second confirms. Confirming redirects to Plants.
  async archive(): Promise<void> {
    await this.askToArchive();
    await this.foot().getByRole('button', { name: 'Archive' }).click();
    await this.page.waitForURL('/plants');
  }

  // The confirmation's Archive button has the same accessible name as the
  // button clicked here. Waiting for the confirmation's Cancel first stops the
  // caller's next click pressing this button again before the swap lands.
  async askToArchive(): Promise<void> {
    await this.foot().getByRole('button', { name: 'Archive' }).click();
    await this.foot().getByRole('button', { name: 'Cancel' }).waitFor();
  }

  async cancelArchive(): Promise<void> {
    const cancel = this.foot().getByRole('button', { name: 'Cancel' });
    await cancel.click();
    await cancel.waitFor({ state: 'detached' });
  }

  // The confirmation and the buttons share this id. The server names it as the
  // swap target.
  foot(): Locator {
    return this.page.locator('#plant-foot');
  }

  // Restore has no confirmation step. With JavaScript it swaps the page under
  // the top bar. Without JavaScript the post redirects back to the page.
  async restore(): Promise<void> {
    const restore = this.page.getByRole('button', { name: 'Restore' });
    await restore.click();
    await restore.waitFor({ state: 'detached' });
  }

  // Log care is a link without JavaScript, so the click is a navigation.
  async logCare(): Promise<void> {
    await this.page.getByRole('link', { name: 'Log care' }).click();
    await this.page.getByRole('dialog').waitFor();
  }
}
