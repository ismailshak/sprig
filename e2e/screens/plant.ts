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

  // A schedule row is found by the care type it names, the only part of it
  // that does not change when the plant is watered.
  scheduleRow(care: string): Locator {
    return this.section('Schedule').getByRole('listitem').filter({ hasText: care });
  }

  // The whole row is the control, so opening the editor is a request either
  // way: swapped in place by htmx and navigated to by a browser running no
  // script.
  async editSchedule(care: string): Promise<void> {
    await this.scheduleRow(care).getByRole('link').click();
    await this.page.waitForLoadState();
  }

  // Every control in the editor is named by its care type as well as by what
  // it is. The add form's rows carry the same names.
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

  // Remove asks before it acts. The first press swaps the editor's foot for the
  // question and the second answers it.
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

  // The link under Recent, which opens the activity log filtered to this plant.
  allActivity(): Locator {
    return this.section('Recent').getByRole('link');
  }

  async edit(): Promise<void> {
    await this.page.getByRole('link', { name: 'Edit plant' }).click();
    await this.page.waitForLoadState();
  }

  // Archive asks before it acts. The first press swaps the foot for the
  // question and the second answers it.
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

  // The question and the two controls share this id, which the server names as
  // the target of the swap.
  foot(): Locator {
    return this.page.locator('#plant-foot');
  }

  // Log care is a link without JavaScript, so the click is a navigation.
  async logCare(): Promise<void> {
    await this.page.getByRole('link', { name: 'Log care' }).click();
    await this.page.waitForLoadState();
  }
}
