import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

export class PlantsScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/plants');
  }

  // A room section is found by its heading, because the id the server gives the
  // section is positional, not the room name.
  room(name: string): Locator {
    return this.page.getByRole('region', { name });
  }

  roomHeadings(): Locator {
    return this.page.getByRole('heading', { level: 2 });
  }

  rowsIn(room: string): Locator {
    return this.room(room).getByRole('listitem');
  }

  // The pattern is anchored at the start because a plant's name can appear as
  // another plant's second line, as Golden pothos does under Trail Mix. An
  // unanchored match would find both rows.
  row(plant: Plant): Locator {
    return this.rowNamed(plant.name);
  }

  rowNamed(name: string): Locator {
    return this.page.getByRole('link', { name: new RegExp(`^${escapePattern(name)}(\\s|$)`) });
  }

  // The picture in the row's circle, found by tag because the image has no alt
  // text of its own.
  picture(name: string): Locator {
    return this.rowNamed(name).locator('img');
  }

  // The link at the bottom of the list to the Archived plants page. Its text
  // is the count, such as "3 archived".
  archiveLink(): Locator {
    return this.page.getByRole('link', { name: /\d+ archived$/ });
  }
}

// The es2024 target predates RegExp.escape.
function escapePattern(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
