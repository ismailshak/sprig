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

  referenceLabels(): Locator {
    return this.section('Reference').getByRole('term');
  }

  referenceValues(): Locator {
    return this.section('Reference').getByRole('definition');
  }

  recentLines(): Locator {
    return this.section('Recent').getByRole('listitem');
  }

  // Log care is a link without JavaScript, so the click is a navigation.
  async logCare(): Promise<void> {
    await this.page.getByRole('link', { name: 'Log care' }).click();
    await this.page.waitForLoadState();
  }
}
