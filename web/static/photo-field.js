/* The photo field on the plant form. The server renders it hidden and this
   script shows it once it has checked the browser for the APIs a resize
   needs. The server never decodes an image, so on a browser without them the
   field stays hidden and the plant is added with no photo.

   A chosen photo is re-encoded into two JPEGs and put in the form's file
   inputs: one bounded to 2048 pixels on its long edge under photo, and a 192
   pixel square cut from its middle under photo-square. Re-encoding drops the
   EXIF block the camera wrote, including the GPS position. */
(function () {
  const field = document.getElementById('photo-field');
  if (!field) return;
  const input = document.getElementById('photo');
  const square = document.getElementById('photo-square');
  const add = document.getElementById('photo-add');
  const chosen = document.getElementById('photo-chosen');
  const preview = document.getElementById('photo-preview');
  const replace = document.getElementById('photo-replace');
  const remove = document.getElementById('photo-remove');
  const error = document.getElementById('photo-error');
  // removeFlag is the hidden input the server reads to clear the plant's
  // stored profile picture.
  const removeFlag = document.getElementById('photo-removed');
  // again is the "The photo needs choosing again." line the server renders
  // when a post with a photo was refused for another field. Choosing a photo
  // removes it.
  const again = document.getElementById('photo-again');

  const canResize =
    typeof HTMLCanvasElement.prototype.toBlob === 'function' &&
    typeof HTMLImageElement.prototype.decode === 'function' &&
    typeof Blob.prototype.arrayBuffer === 'function' &&
    typeof DataTransfer === 'function' &&
    typeof URL.createObjectURL === 'function';
  if (!canResize) return;
  field.hidden = false;

  const longEdge = 2048;
  const squareEdge = 192;
  // 0.85 puts a 2048 pixel photo at around 350KB.
  const quality = 0.85;

  // previewURL is the blob: URL the preview <img> shows. It is empty when no
  // photo is chosen.
  let previewURL = '';

  // decode returns an <img> holding the file at url. The browser applies the
  // EXIF orientation tag as it decodes, so the canvas below is drawn from
  // upright pixels.
  const decode = (url) => {
    const img = new Image();
    img.src = url;
    return img.decode().then(() => img);
  };

  // withoutMetadata returns the JPEG in blob with its APP1 and APP13 segments
  // cut out. APP1 holds EXIF and XMP, APP13 holds Photoshop and IPTC data.
  // Safari's encoder writes a fresh one of each into every JPEG it makes, so
  // they are cut rather than assumed absent. The loop walks the segments: each
  // is a 0xFF byte, a marker byte and a two-byte length that counts itself, up
  // to the start-of-scan marker where the image data begins.
  const withoutMetadata = async (blob) => {
    const bytes = new Uint8Array(await blob.arrayBuffer());
    const kept = [];
    let start = 0;
    let offset = 2;
    while (offset + 4 <= bytes.length && bytes[offset] === 0xff) {
      const marker = bytes[offset + 1];
      if (marker === 0xda) break;
      const length = 2 + ((bytes[offset + 2] << 8) | bytes[offset + 3]);
      if (marker === 0xe1 || marker === 0xed) {
        kept.push(bytes.subarray(start, offset));
        start = offset + length;
      }
      offset += length;
    }
    kept.push(bytes.subarray(start));
    return new Blob(kept, { type: 'image/jpeg' });
  };

  const encode = async (img, crop, width, height, name) => {
    const canvas = document.createElement('canvas');
    canvas.width = width;
    canvas.height = height;
    const ctx = canvas.getContext('2d');
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(img, crop.x, crop.y, crop.width, crop.height, 0, 0, width, height);
    const encoded = await new Promise((resolve, reject) => {
      canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('encoding failed'))), 'image/jpeg', quality);
    });
    return new File([await withoutMetadata(encoded)], name, { type: 'image/jpeg' });
  };

  // resize returns the two files the form posts for img.
  const resize = async (img, stem) => {
    const width = img.naturalWidth;
    const height = img.naturalHeight;
    const scale = Math.min(1, longEdge / Math.max(width, height));
    const whole = { x: 0, y: 0, width, height };
    const side = Math.min(width, height);
    const middle = { x: (width - side) / 2, y: (height - side) / 2, width: side, height: side };
    return {
      photo: await encode(img, whole, Math.round(width * scale), Math.round(height * scale), stem + '.jpg'),
      square: await encode(img, middle, squareEdge, squareEdge, stem + '-square.jpg'),
    };
  };

  // hold puts file into the file input target. A FileList cannot be
  // constructed, so DataTransfer builds the one assigned to target.files.
  const hold = (target, file) => {
    const transfer = new DataTransfer();
    transfer.items.add(file);
    target.files = transfer.files;
  };

  const clear = () => {
    input.value = '';
    square.value = '';
    if (previewURL) URL.revokeObjectURL(previewURL);
    previewURL = '';
    preview.removeAttribute('src');
    chosen.hidden = true;
    add.hidden = false;
  };

  const show = (photo) => {
    if (previewURL) URL.revokeObjectURL(previewURL);
    previewURL = URL.createObjectURL(photo);
    preview.src = previewURL;
    preview.alt = 'Chosen photo';
    removeFlag.value = '';
    add.hidden = true;
    chosen.hidden = false;
  };

  input.addEventListener('change', async () => {
    error.hidden = true;
    if (again) again.remove();
    // An input with no file is cleared so the field shows what will be posted.
    const file = input.files[0];
    if (!file) {
      clear();
      return;
    }
    const url = URL.createObjectURL(file);
    try {
      const img = await decode(url);
      const stem = file.name.replace(/\.[^.]*$/, '') || 'photo';
      const { photo, square: variant } = await resize(img, stem);
      hold(input, photo);
      hold(square, variant);
      show(photo);
    } catch {
      clear();
      error.hidden = false;
    } finally {
      URL.revokeObjectURL(url);
    }
  });

  add.addEventListener('click', () => input.click());
  replace.addEventListener('click', () => input.click());
  remove.addEventListener('click', () => {
    error.hidden = true;
    removeFlag.value = '1';
    clear();
  });
})();
