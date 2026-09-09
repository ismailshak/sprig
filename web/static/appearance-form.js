/* The Appearance page's three radio buttons: light, dark and system. The
   choice is kept in localStorage and never posted, because it belongs to this
   browser rather than the account. The server renders the radios disabled, and
   this script checks the stored one, enables them and removes the line under
   them.

   The head of every page reads the same key before the first paint, so the
   choice made here holds on the next page too. */
(function () {
  const form = document.getElementById('appearance-form');
  if (!form) return;
  const note = document.getElementById('appearance-noscript');

  let stored = null;
  try {
    stored = localStorage.getItem('appearance');
  } catch {
    // Storage is refused, so the choice cannot be kept. The radios stay
    // disabled and the line stays.
    return;
  }
  const mode = stored === 'light' || stored === 'dark' ? stored : 'system';

  for (const radio of form.elements.mode) {
    radio.checked = radio.value === mode;
    radio.disabled = false;
  }
  if (note) note.remove();

  form.addEventListener('change', () => {
    const chosen = form.elements.mode.value;
    try {
      if (chosen === 'system') {
        localStorage.removeItem('appearance');
      } else {
        localStorage.setItem('appearance', chosen);
      }
    } catch {
      // The page changes mode for as long as it is open, and the next page
      // follows the system again.
    }
    if (chosen === 'system') {
      delete document.documentElement.dataset.mode;
    } else {
      document.documentElement.dataset.mode = chosen;
    }
  });
})();
