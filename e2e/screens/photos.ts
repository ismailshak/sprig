import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

// The URL of a plant's Photos page.
export const photosPage = /\/plants\/[0-9a-f-]{36}\/photos$/;

// The URL of one photo's own page. Matching the id keeps it from matching
// /photos/new.
export const photoPage = /\/plants\/[0-9a-f-]{36}\/photos\/[0-9a-f-]{36}$/;

// A plant's Photos page, the grid of every photo newest first.
export class PhotosScreen {
  constructor(private readonly page: Page) {}

  async open(plant: Plant): Promise<void> {
    await this.page.goto(`/plants/${plant.id}/photos`);
  }

  // Each photo in the grid is a link to its own page, named by its image.
  tiles(): Locator {
    return this.page.getByRole('link', { name: /^Photo of / });
  }

  async openTile(index: number): Promise<void> {
    await this.tiles().nth(index).click();
    await this.page.waitForURL(photoPage);
  }

  // The tile at the head of the grid for a member who may add photos.
  addTile(): Locator {
    return this.page.getByRole('link', { name: 'Add', exact: true });
  }

  // The action under "No photos yet" for a member who may add photos.
  addPhoto(): Locator {
    return this.page.getByRole('link', { name: 'Add photo' });
  }

  // The back link, named after the plant.
  back(plant: Plant): Locator {
    return this.page.getByRole('link', { name: plant.name, exact: true });
  }
}
