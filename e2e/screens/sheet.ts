import type { Locator, Page } from '@playwright/test';

export class SheetScreen {
  constructor(private readonly page: Page) {}

  dialog(): Locator {
    return this.page.getByRole('dialog');
  }

  // Every chip on the sheet is a radio: the care type, the outcome, when it
  // happened and the reminder.
  chip(label: string): Locator {
    return this.dialog().getByRole('radio', { name: label, exact: true });
  }

  time(): Locator {
    return this.dialog().getByLabel('Time', { exact: true });
  }

  note(): Locator {
    return this.dialog().getByLabel('Note');
  }

  // A fieldset on the sheet, named by its legend: Care, Outcome or When. With
  // JavaScript, a refusal's text is also written into announcement() 100ms
  // after the swap, so a test looks for the refusal inside its field.
  field(legend: string): Locator {
    return this.dialog().getByRole('group', { name: legend, exact: true });
  }

  // Only one primary button shows, "Log watering" or "Log skip".
  async submit(label: string): Promise<void> {
    await this.dialog().getByRole('button', { name: label, exact: true }).click();
  }

  // Opened over an event already logged, the sheet saves rather than logs and
  // Delete is beside the primary button.
  deleteButton(): Locator {
    return this.dialog().getByRole('button', { name: 'Delete' });
  }

  loggedBy(): Locator {
    return this.dialog().getByText(/^Logged by /);
  }

  // The live region inside the sheet. While the sheet is open, the page's
  // script writes announcements and failed requests into it instead of #status.
  announcement(): Locator {
    return this.dialog().getByRole('status');
  }

  // dragDown dispatches a ten-move touch drag down from the middle of target.
  // Playwright's touchscreen can only tap.
  async dragDown(target: Locator, distance: number): Promise<void> {
    const box = await target.boundingBox();
    if (!box) throw new Error('dragDown: the target is not on screen');
    const x = box.x + box.width / 2;
    const top = box.y + box.height / 2;
    const touch = (y: number) => ({ identifier: 0, clientX: x, clientY: y });
    await target.dispatchEvent('touchstart', { touches: [touch(top)], changedTouches: [touch(top)] });
    for (let step = 1; step <= 10; step++) {
      const at = touch(top + (distance * step) / 10);
      await target.dispatchEvent('touchmove', { touches: [at], changedTouches: [at] });
    }
    await target.dispatchEvent('touchend', { touches: [], changedTouches: [touch(top + distance)] });
  }

  cancelButton(): Locator {
    return this.dialog().getByRole('button', { name: 'Cancel' });
  }

  async cancel(): Promise<void> {
    await this.cancelButton().click();
  }
}
