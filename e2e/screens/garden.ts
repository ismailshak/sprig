import type { Locator, Page } from '@playwright/test';

export class GardenScreen {
  constructor(private readonly page: Page) {}

  async open(): Promise<void> {
    await this.page.goto('/more/garden');
  }

  // The hint is inside the label, so the field's accessible name is "Name what
  // the top of Today says".
  name(): Locator {
    return this.page.getByLabel(/^Name\b/);
  }

  saveName(): Locator {
    return this.page.getByRole('button', { name: 'Save the name' });
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
    return this.page.getByRole('link', { name: 'Add a care type' });
  }

  // The open row's field. Only one row is open at a time, so the page has one.
  typeName(): Locator {
    return this.page.getByLabel('Care type name');
  }

  save(): Locator {
    return this.page.getByRole('button', { name: 'Save', exact: true });
  }

  cancel(): Locator {
    return this.page.getByRole('link', { name: 'Cancel' });
  }

  // Turn it off, Turn it back on or Delete, whichever the open row offers.
  drop(label: string): Locator {
    return this.page.getByRole('button', { name: label, exact: true });
  }
}
