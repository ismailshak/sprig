/* Sets data-js on the html element, data-mode from the Appearance choice
   stored in this browser, and photo--loaded on each photo once it has loaded,
   so the page paints in the chosen mode from its first frame and a photo the
   browser already holds is on it.
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

  let mode = null;
  try {
    mode = localStorage.getItem('appearance');
  } catch {
    // Storage can be refused, in a private window for example. The page then
    // follows the system.
  }
  if (mode !== 'light' && mode !== 'dark') return;
  document.documentElement.dataset.mode = mode;

  // Each theme-color tag has a prefers-color-scheme media query, so an
  // installed app on a light system would keep a light status bar over a dark
  // page. The tag for the chosen mode is kept with its media query removed, so
  // it applies whatever the system says.
  for (const meta of document.querySelectorAll('meta[name="theme-color"]')) {
    if (meta.media === '(prefers-color-scheme: ' + mode + ')') {
      meta.removeAttribute('media');
    } else {
      meta.remove();
    }
  }
})();
