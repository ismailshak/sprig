import type { Locator, Page } from '@playwright/test';
import type { Plant } from '../harness/garden';
import { photosPage } from './photos';

// The Add a photo page.
export class PhotoFormScreen {
  constructor(private readonly page: Page) {}

  async open(plant: Plant): Promise<void> {
    await this.page.goto(`/plants/${plant.id}/photos/new`);
  }

  // The Choose photo button. The server renders it hidden and the page's
  // script shows it, so with JavaScript off it stays hidden.
  chooseButton(): Locator {
    return this.page.getByRole('button', { name: 'Choose photo' });
  }

  async choose(file: string): Promise<void> {
    const chooser = this.page.waitForEvent('filechooser');
    await this.chooseButton().click();
    await (await chooser).setFiles(file);
  }

  // The resized photo, shown before it is sent.
  preview(): Locator {
    return this.page.getByRole('img', { name: 'Chosen photo' });
  }

  // The line shown in a browser that cannot resize a photo. The page's script
  // hides it when the browser can.
  unsupported(): Locator {
    return this.page.getByText('Photos can’t be added from this device.');
  }

  submitButton(): Locator {
    return this.page.getByRole('button', { name: 'Add photo' });
  }

  // A photo that is added redirects to the plant's Photos page.
  async submit(): Promise<void> {
    await this.submitButton().click();
    await this.page.waitForURL(photosPage);
  }
}
