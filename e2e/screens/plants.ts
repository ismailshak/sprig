import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

export class PlantsScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/plants');
  }

  // A room is a region whose accessible name is its heading because the id the
  // server puts on the section is a position rather than the room.
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
    return this.page.getByRole('link', { name: new RegExp(`^${escapePattern(plant.name)}(\\s|$)`) });
  }
}

// The es2024 target predates RegExp.escape.
function escapePattern(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
