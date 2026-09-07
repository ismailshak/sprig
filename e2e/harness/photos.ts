import type { Locator } from '@playwright/test';
import { fileURLToPath } from 'node:url';

// The photos a test chooses on the plant form.
export const photos = {
  // 3000 by 2000 pixels, with an EXIF block holding a GPS position.
  gpsTagged: fileURLToPath(new URL('../fixtures/gps-tagged.jpg', import.meta.url)),
  // 1200 by 800 pixels with an EXIF orientation of 6, the way a phone stores a
  // portrait photo. Shown upright it is 800 wide and 1200 high.
  sideways: fileURLToPath(new URL('../fixtures/sideways.jpg', import.meta.url)),
};

// heldFile reads back the bytes of the file a file input holds. That is what
// the form posts under the input's name.
export async function heldFile(input: Locator): Promise<Buffer> {
  const dataURL = await input.evaluate((element: HTMLInputElement) => {
    const file = element.files?.[0];
    if (!file) throw new Error('the input holds no file');
    return new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result as string);
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(file);
    });
  });
  return Buffer.from(dataURL.slice(dataURL.indexOf(',') + 1), 'base64');
}

export function heldCount(input: Locator): Promise<number> {
  return input.evaluate((element: HTMLInputElement) => element.files?.length ?? 0);
}

// loaded reports whether the browser fetched and decoded the image at img's
// src. A src that 404s leaves a visible element with nothing in it, so
// toBeVisible passes on an image that never loaded.
export function loaded(img: Locator): Promise<boolean> {
  return img.evaluate((element: HTMLImageElement) => element.complete && element.naturalWidth > 0);
}

// A JPEG is a run of segments after the two-byte start-of-image marker. Each
// begins with 0xFF and a marker byte. All but the restart markers then have a
// two-byte length that counts itself. The walk stops at the start-of-scan
// segment, after which the bytes are compressed pixel data.
type Segment = { marker: number; offset: number; length: number };

function segments(jpeg: Buffer): Segment[] {
  const found: Segment[] = [];
  let offset = 2;
  while (offset + 4 <= jpeg.length) {
    if (jpeg[offset] !== 0xff) throw new Error(`no marker at byte ${offset}`);
    const marker = jpeg[offset + 1];
    if (marker >= 0xd0 && marker <= 0xd7) {
      offset += 2;
      continue;
    }
    const length = jpeg.readUInt16BE(offset + 2);
    found.push({ marker, offset, length });
    if (marker === 0xda) break;
    offset += 2 + length;
  }
  return found;
}

// hasExif reports whether the JPEG has an APP1 segment. APP1 is where EXIF
// goes, a phone's GPS position included.
export function hasExif(jpeg: Buffer): boolean {
  return segments(jpeg).some((segment) => segment.marker === 0xe1);
}

// dimensions reads the pixel size from the start-of-frame segment.
export function dimensions(jpeg: Buffer): { width: number; height: number } {
  const frame = segments(jpeg).find(
    (segment) => segment.marker >= 0xc0 && segment.marker <= 0xcf && ![0xc4, 0xc8, 0xcc].includes(segment.marker),
  );
  if (!frame) throw new Error('no start-of-frame segment');
  return { height: jpeg.readUInt16BE(frame.offset + 5), width: jpeg.readUInt16BE(frame.offset + 7) };
}
