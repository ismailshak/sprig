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

  section(title: 'Schedule' | 'Reference' | 'Photos' | 'Recent'): Locator {
    return this.page.getByRole('region', { name: title });
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

  async askToRemoveSchedule(care: string): Promise<void> {
    await this.scheduleRow(care).getByRole('link', { name: 'Remove' }).click();
    await this.page.waitForLoadState();
  }

  async keepSchedule(care: string): Promise<void> {
    await this.scheduleRow(care).getByRole('button', { name: 'Keep it' }).click();
    await this.page.waitForLoadState();
  }

  referenceLabels(): Locator {
    return this.section('Reference').getByRole('term');
  }

  referenceValues(): Locator {
    return this.section('Reference').getByRole('definition');
  }

  recentLines(): Locator {
    return this.section('Recent').getByRole('listitem');
  }

  // The link under Recent. It opens the activity log filtered to this plant.
  allActivity(): Locator {
    return this.section('Recent').getByRole('link');
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

  async askToArchive(): Promise<void> {
    await this.page.getByRole('button', { name: 'Archive' }).click();
    await this.page.waitForLoadState();
  }

  async keepIt(): Promise<void> {
    await this.foot().getByRole('button', { name: 'Keep it' }).click();
    await this.page.waitForLoadState();
  }

  // The confirmation and the buttons share this id. The server names it as the
  // swap target.
  foot(): Locator {
    return this.page.locator('#plant-foot');
  }

  // Log care is a link without JavaScript, so the click is a navigation.
  async logCare(): Promise<void> {
    await this.page.getByRole('link', { name: 'Log care' }).click();
    await this.page.waitForLoadState();
  }
}
