/* Sets data-js on the html element, and data-mode from the Appearance choice
   stored in this browser, so the page paints in the chosen mode from its first
   frame.
   The layout loads this script blocking in the head, because a deferred
   script would run after the first paint and the system's mode would show
   for a frame. It is a file rather than an inline script because the
   Content-Security-Policy allows no inline script.

   The Appearance page writes localStorage's "appearance" as "light" or
   "dark". No value means follow the system, and the stylesheet does that when
   the attribute is absent. */
(function () {
  // The js attribute on the html element tells the stylesheet that scripts
  // run, so a photo can be hidden until the page's script says it has loaded.
  document.documentElement.dataset.js = '';

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
