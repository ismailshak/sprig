import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';
import { plantPage } from './plant';

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

  // A label may have a hint after the field name, so this matches on the start
  // of it. The word boundary keeps Room from matching the Rooms listbox.
  field(name: string): Locator {
    return this.page.getByLabel(new RegExp(`^${name}\\b`));
  }

  // The listbox of rooms under the Room field. It is empty until the field
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
    await this.row(care).getByRole('button', { name: 'Don’t schedule' }).waitFor();
  }

  // dontSchedule clicks Don't schedule and waits for the row to show Not
  // scheduled. With JavaScript the click is a swap. A form posted before the
  // swap has replaced the row still posts the open row's fields.
  async dontSchedule(care: string): Promise<void> {
    await this.row(care).getByRole('button', { name: 'Don’t schedule' }).click();
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

  // The Add photo button. The server renders it hidden and the page's
  // script shows it, so with JavaScript off it stays hidden.
  addPhoto(): Locator {
    return this.page.getByRole('button', { name: 'Add photo' });
  }

  photoPreview(): Locator {
    return this.page.getByRole('img', { name: 'Chosen photo' });
  }

  // The plant's stored picture, shown in the photo field when the edit form
  // opens on a plant that has one.
  currentPicture(): Locator {
    return this.page.getByRole('img', { name: 'Current photo' });
  }

  // The box the form's script puts over the photo preview, marking the part
  // the plant's page shows. Its value is 0 at the top or left edge of the
  // photo and 100 at the bottom or right, on the one axis it moves along.
  frame(): Locator {
    return this.page.getByRole('slider', { name: /^Part shown on the plant/ });
  }

  // Drags the frame by a share of its own height, or width when it moves
  // sideways. A positive share drags down or right.
  async dragFrame(share: number): Promise<void> {
    const box = await this.frame().boundingBox();
    if (!box) throw new Error('the frame has no box on the page');
    const vertical = (await this.frame().getAttribute('aria-orientation')) === 'vertical';
    const from = { x: box.x + box.width / 2, y: box.y + box.height / 2 };
    const to = vertical ? { x: from.x, y: from.y + box.height * share } : { x: from.x + box.width * share, y: from.y };
    await this.page.mouse.move(from.x, from.y);
    await this.page.mouse.down();
    await this.page.mouse.move(to.x, to.y, { steps: 5 });
    await this.page.mouse.up();
  }

  async choosePhoto(button: 'Add photo' | 'Replace', file: ChosenFile): Promise<void> {
    const chooser = this.page.waitForEvent('filechooser');
    await this.page.getByRole('button', { name: button }).click();
    await (await chooser).setFiles(file);
  }

  async removePhoto(): Promise<void> {
    await this.page.getByRole('button', { name: 'Remove' }).click();
  }

  // The hidden file input the form posts under that name. photo holds the
  // resized photo and photo-square its square.
  photoInput(name: 'photo' | 'photo-square'): Locator {
    return this.page.locator(`input[name="${name}"]`);
  }

  submitButton(button: 'Add plant' | 'Save changes'): Locator {
    return this.page.getByRole('button', { name: button });
  }

  // A plant that is saved redirects to its own page. A refused form renders on
  // the URL it posted to, so a test of a refusal clicks submitButton instead.
  async submit(button: 'Add plant' | 'Save changes'): Promise<void> {
    await this.submitButton(button).click();
    await this.page.waitForURL(plantPage);
  }
}
