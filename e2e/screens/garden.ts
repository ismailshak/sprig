import type { Locator, Page } from '@playwright/test';

export class GardenScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/garden');
  }

  name(): Locator {
    return this.page.getByLabel('Name', { exact: true });
  }

  // The Save button under the garden's name. The open care type row has a Save
  // of its own, so this one is found through the form the name field is in.
  saveName(): Locator {
    return this.page.locator('form', { has: this.name() }).getByRole('button', { name: 'Save' });
  }

  // A closed row is a link to the care type's own URL, and its text is the
  // care type's name followed by Off when it has been turned off. The word
  // boundary keeps Water from matching Watering after a rename. care is used
  // as a pattern, so a name with a regular expression character in it would
  // not match.
  row(care: string): Locator {
    return this.page.getByRole('link', { name: new RegExp(`^${care}\\b`) });
  }

  addType(): Locator {
    return this.page.getByRole('link', { name: 'Add care type' });
  }

  // The open row's field. Only one row is open at a time, so the page has one.
  typeName(): Locator {
    return this.page.getByLabel('Care type name');
  }

  save(): Locator {
    return this.page.locator('form', { has: this.typeName() }).getByRole('button', { name: 'Save' });
  }

  cancel(): Locator {
    return this.page.getByRole('link', { name: 'Cancel' });
  }

  // Turn off, Turn on or Delete, whichever the open row offers.
  drop(label: string): Locator {
    return this.page.getByRole('button', { name: label, exact: true });
  }

  // The sentence under Photos saying how much of the garden's photo storage
  // is used.
  storage(): Locator {
    return this.page.getByText(/of photo storage used\./);
  }

  deleteGarden(): Locator {
    return this.page.getByRole('link', { name: 'Delete garden' });
  }
}
