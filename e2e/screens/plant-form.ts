import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

// One screen object for two routes, because adding a plant and editing one are
// the same fields in the same order.
export class PlantFormScreen {
  constructor(private readonly page: Page) {}

  async openNew(): Promise<void> {
    await this.page.goto('/plants/new');
  }

  async openEdit(plant: Plant): Promise<void> {
    await this.page.goto(`/plants/${plant.id}/edit`);
  }

  // The label has a hint after the field name, so this matches on part of it.
  field(name: string): Locator {
    return this.page.getByLabel(name);
  }

  // A schedule row is found by its care type, the one part that does not change
  // when the row opens.
  row(care: string): Locator {
    return this.page.getByRole('listitem').filter({ hasText: care });
  }

  // Opening a row is a request either way. With JavaScript it is an htmx swap,
  // without it a navigation.
  async schedule(care: string): Promise<void> {
    await this.row(care).getByRole('button', { name: 'Not scheduled' }).click();
    await this.page.waitForLoadState();
  }

  async dontSchedule(care: string): Promise<void> {
    await this.row(care).getByRole('button', { name: "Don't schedule" }).click();
    await this.page.waitForLoadState();
  }

  // Every control's label includes its care type, because more than one row can
  // be open at once.
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

  async submit(button: 'Add plant' | 'Save changes'): Promise<void> {
    await this.page.getByRole('button', { name: button }).click();
    await this.page.waitForLoadState();
  }
}
