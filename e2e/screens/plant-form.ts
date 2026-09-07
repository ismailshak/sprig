import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';

// The file to give the picker: a path on disk, or bytes made up in the test.
type ChosenFile = string | { name: string; mimeType: string; buffer: Buffer };

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

  // The listbox of rooms under the Location field. It is empty until the field
  // is focused and the script fills it, so it stays empty in a browser running
  // no script.
  roomList(): Locator {
    return this.page.getByRole('listbox', { name: 'Rooms' });
  }

  roomOptions(): Locator {
    return this.roomList().getByRole('option');
  }

  roomOption(name: string): Locator {
    return this.roomList().getByRole('option', { name, exact: true });
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

  // dontSchedule clicks Don't schedule and waits for the row to show Not
  // scheduled. With JavaScript the click is a swap. A form posted before the
  // swap has replaced the row still carries the open row's fields.
  async dontSchedule(care: string): Promise<void> {
    await this.row(care).getByRole('button', { name: "Don't schedule" }).click();
    await this.row(care).getByRole('button', { name: 'Not scheduled' }).waitFor();
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

  // The Add a photo button. The server renders it hidden and the page's
  // script shows it, so with JavaScript off it stays hidden.
  addPhoto(): Locator {
    return this.page.getByRole('button', { name: 'Add a photo' });
  }

  photoPreview(): Locator {
    return this.page.getByRole('img', { name: 'Chosen photo' });
  }

  // The plant's stored picture, shown in the photo field when the edit form
  // opens on a plant that has one.
  currentPicture(): Locator {
    return this.page.getByRole('img', { name: 'Current picture' });
  }

  async choosePhoto(button: 'Add a photo' | 'Replace', file: ChosenFile): Promise<void> {
    const chooser = this.page.waitForEvent('filechooser');
    await this.page.getByRole('button', { name: button }).click();
    await (await chooser).setFiles(file);
  }

  async removePhoto(): Promise<void> {
    await this.page.getByRole('button', { name: 'Remove' }).click();
  }

  // The hidden file input the form posts under that name. photo holds the
  // resized photo and photo-square its 192 pixel square.
  photoInput(name: 'photo' | 'photo-square'): Locator {
    return this.page.locator(`input[name="${name}"]`);
  }

  async submit(button: 'Add plant' | 'Save changes'): Promise<void> {
    await this.page.getByRole('button', { name: button }).click();
    await this.page.waitForLoadState();
  }
}
