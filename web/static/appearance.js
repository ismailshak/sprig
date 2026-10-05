/* Sets data-js on the html element, sets data-mode and the theme-color tags
   from the Appearance choice stored in this browser, and adds photo--loaded to
   each photo once it has loaded.
   The layout loads this script blocking in the head, because a deferred
   script would run after the first paint: the system's mode would show for a
   frame, and a cached photo would be painted blank and then fade in. It is a
   file rather than an inline script because the Content-Security-Policy
   allows no inline script.

   The Appearance page writes localStorage's "appearance" as "light" or
   "dark". No value means follow the system, and the stylesheet does that when
   the attribute is absent. */
(function () {
  // The js attribute on the html element tells the stylesheet that scripts
  // run, so a photo can be hidden until this script says it has loaded.
  document.documentElement.dataset.js = '';

  // painted is false until the first frame. A photo that loads after it was
  // painted blank, so it fades in. One that loads before is shown outright.
  let painted = false;
  requestAnimationFrame(() => (painted = true));
  // load and error do not bubble, so they are caught in the capture phase.
  // The listener is on the document, so it also covers the photos the lazy
  // grid swaps in later. A photo is hidden until it has all its bytes,
  // because a browser paints a JPEG top down as they arrive.
  const loaded = (event) => {
    const img = event.target;
    if (!(img instanceof HTMLImageElement) || !img.hasAttribute('data-photo')) return;
    if (painted) img.classList.add('photo--fade');
    img.classList.add('photo--loaded');
  };
  document.addEventListener('load', loaded, true);
  document.addEventListener('error', loaded, true);

  // themeColors is the theme-color tags with the content and media the server
  // rendered, one tag for each prefers-color-scheme. Choosing System after
  // Light or Dark puts these values back.
  const themeColors = Array.from(document.querySelectorAll('meta[name="theme-color"]'), (meta) => ({
    meta,
    content: meta.content,
    media: meta.media,
  }));

  // window.sprigAppearance sets data-mode and the theme-color tags for mode.
  // mode is "light", "dark" or "system". The Appearance page calls it when the
  // choice changes.
  //
  // For Light or Dark, both tags get that mode's colour and lose their
  // prefers-color-scheme media query. With the query, an installed app on a
  // light system would keep a light status bar over a dark page.
  window.sprigAppearance = (mode) => {
    if (mode === 'light' || mode === 'dark') {
      document.documentElement.dataset.mode = mode;
    } else {
      delete document.documentElement.dataset.mode;
    }
    const chosen = themeColors.find((tag) => tag.media === '(prefers-color-scheme: ' + mode + ')');
    for (const tag of themeColors) {
      if (chosen) {
        tag.meta.content = chosen.content;
        tag.meta.removeAttribute('media');
      } else {
        tag.meta.content = tag.content;
        tag.meta.media = tag.media;
      }
    }
  };

  let mode = null;
  try {
    mode = localStorage.getItem('appearance');
  } catch {
    // Storage can be refused, in a private window for example. The page then
    // follows the system.
  }
  if (mode === 'light' || mode === 'dark') window.sprigAppearance(mode);
})();
