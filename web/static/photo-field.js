/* The photo field on the plant form and on Add a photo. The server renders it
   hidden and this script shows it once it has checked the browser for the
   APIs a resize needs. The server never decodes an image, so on a browser
   without them the field stays hidden. The plant form then saves the plant
   with no photo, and Add a photo shows a line saying a photo cannot be added
   from that browser.

   A chosen photo is re-encoded into two JPEGs and put in the form's file
   inputs: one bounded to 2048 pixels on its long edge under photo, and a 320
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
  // removeFlag is the hidden input the plant form posts to clear the plant's
  // stored profile picture. Add a photo has no picture to clear, so the input
  // is not on that page.
  const removeFlag = document.getElementById('photo-removed');
  // unsupported is the line Add a photo shows in a browser that cannot resize
  // an image. It is hidden once the check below passes.
  const unsupported = document.getElementById('photo-unsupported');
  // again is the "The photo needs choosing again." line the server renders
  // when a post with a photo was refused for another field. Choosing a photo
  // removes it.
  const again = document.getElementById('photo-again');
  // focus is the hidden input the plant form posts the picture's focal point
  // in, "x,y" in percentages. Add a photo has no such input, so no frame is
  // added there.
  const focus = document.getElementById('photo-focus');
  // preparing is the line shown while a chosen photo is being resized. The
  // form's submit buttons are disabled for as long as it shows, so the post
  // cannot go out before the file inputs hold the resized photo.
  const preparing = document.getElementById('photo-preparing');
  const submits = field.closest('form').querySelectorAll('button[type="submit"]');
  const busy = (on) => {
    preparing.hidden = !on;
    for (const button of submits) button.disabled = on;
  };

  const canResize =
    typeof HTMLCanvasElement.prototype.toBlob === 'function' &&
    typeof HTMLImageElement.prototype.decode === 'function' &&
    typeof Blob.prototype.arrayBuffer === 'function' &&
    typeof DataTransfer === 'function' &&
    typeof URL.createObjectURL === 'function';
  if (!canResize) return;
  field.hidden = false;
  if (unsupported) unsupported.hidden = true;

  const longEdge = 2048;
  const squareEdge = 320;
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
    if (removeFlag) removeFlag.value = '';
    if (focus) focus.value = '50,50';
    add.hidden = true;
    chosen.hidden = false;
  };

  /* addFrame puts a frame over the preview: the 5 by 3 box the plant's page
     shows the picture in, moved to the part of the photo to show. On a photo
     taller than the box the frame is the photo's full width and travels up and
     down. On a wider one it is the full height and travels side to side. The
     focus input holds the frame's place as "x,y" in percentages. */
  const addFrame = () => {
    const crop = document.getElementById('photo-crop');
    crop.classList.add('photo-field__crop--on');

    const frame = document.createElement('div');
    frame.className = 'photo-field__frame';
    frame.tabIndex = 0;
    frame.setAttribute('role', 'slider');
    frame.setAttribute('aria-label', 'Part shown on the plant’s page');
    frame.setAttribute('aria-valuemin', '0');
    frame.setAttribute('aria-valuemax', '100');
    crop.append(frame);

    const boxRatio = 5 / 3;

    // position is the frame's place along the axis it moves on, from 0 at the
    // top or left edge to 100 at the bottom or right. across is true when that
    // axis is horizontal, size the frame's share of the photo along it and
    // travel the share left for the frame to move through.
    let position = 50;
    let across = false;
    let size = 1;
    let travel = 0;

    const place = () => {
      position = Math.min(100, Math.max(0, Math.round(position)));
      const offset = `${travel * position}%`;
      const span = `${size * 100}%`;
      frame.style.left = across ? offset : '0';
      frame.style.top = across ? '0' : offset;
      frame.style.width = across ? span : '100%';
      frame.style.height = across ? '100%' : span;
      frame.setAttribute('aria-valuenow', String(position));
      frame.setAttribute('aria-orientation', across ? 'horizontal' : 'vertical');
      focus.value = across ? `${position},50` : `50,${position}`;
    };

    // fitFrame sizes the frame from the photo's dimensions once they are
    // known. It takes the frame's starting position from the focus input, on
    // the axis the frame moves along.
    const fitFrame = () => {
      const width = preview.naturalWidth;
      const height = preview.naturalHeight;
      if (!width || !height) return;
      across = width / height > boxRatio;
      size = across ? (height * boxRatio) / width : width / boxRatio / height;
      travel = 1 - size;
      const [x, y] = focus.value.split(',').map(Number);
      position = across ? x : y;
      place();
    };
    preview.addEventListener('load', fitFrame);
    fitFrame();

    // grab is where a drag started: the pointer's coordinate along the axis
    // and the frame's position then. Null between drags.
    let grab = null;
    frame.addEventListener('pointerdown', (event) => {
      frame.setPointerCapture(event.pointerId);
      grab = { at: across ? event.clientX : event.clientY, from: position };
      event.preventDefault();
    });
    frame.addEventListener('pointermove', (event) => {
      if (!grab) return;
      const rect = crop.getBoundingClientRect();
      // pixels is the distance the frame can travel on the page.
      const pixels = (across ? rect.width : rect.height) * travel;
      if (!pixels) return;
      const moved = (across ? event.clientX : event.clientY) - grab.at;
      position = grab.from + (moved / pixels) * 100;
      place();
    });
    const release = () => {
      grab = null;
    };
    frame.addEventListener('pointerup', release);
    frame.addEventListener('pointercancel', release);

    frame.addEventListener('keydown', (event) => {
      const step = { ArrowLeft: -5, ArrowUp: -5, ArrowRight: 5, ArrowDown: 5 }[event.key];
      if (event.key === 'Home') position = 0;
      else if (event.key === 'End') position = 100;
      else if (step) position += step;
      else return;
      event.preventDefault();
      place();
    });
  };
  if (focus) addFrame();

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
    busy(true);
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
      busy(false);
    }
  });

  add.addEventListener('click', () => input.click());
  replace.addEventListener('click', () => input.click());
  remove.addEventListener('click', () => {
    error.hidden = true;
    if (removeFlag) removeFlag.value = '1';
    if (focus) focus.value = '50,50';
    clear();
  });
})();
