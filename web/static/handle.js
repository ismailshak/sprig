/* Fills the Handle field on Set up your garden and an invite's join form. As
   the display name is typed the handle mirrors it in the form the server
   stores: lower case letters and digits with single underscores between them,
   at most 32 characters. When the display name is left, the server is asked
   for a handle no account holds. Its answer has a suffix on a name somebody
   else already has. Typing into the handle stops the mirroring and the
   asking. The button beside the field asks again. The server normalises
   whatever is posted, so this copy of the rule cannot change what is stored. */
(function () {
  const name = document.getElementById('name');
  const handle = document.getElementById('handle');
  const again = document.getElementById('handle-again');
  if (!name || !handle || !again) return;
  const form = handle.form;

  // byHand is true once the handle has been typed into, so neither the
  // display name nor the server overwrites it.
  let byHand = handle.value !== '' && handle.value !== handleFor(name.value);

  name.addEventListener('input', () => {
    if (byHand) return;
    handle.value = handleFor(name.value);
  });

  name.addEventListener('change', () => {
    if (byHand) return;
    suggest();
  });

  handle.addEventListener('input', () => {
    byHand = handle.value !== '';
  });

  again.hidden = false;
  again.addEventListener('click', () => {
    byHand = false;
    suggest();
  });

  // pending is the request in flight, or null. A submit while one is in
  // flight is held until it settles and then made again, so the handle posted
  // is the one on the screen. That happens when the submit button is pressed
  // straight after the display name is typed, because leaving the field starts
  // a request the press then beats. The listener is on the capture phase so it
  // runs before the one that turns the submit into a passkey registration.
  let pending = null;
  form.addEventListener(
    'submit',
    (event) => {
      if (!pending) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      pending.then(() => form.requestSubmit());
    },
    true,
  );

  // suggest asks the server for a free handle for the display name. The field
  // is read-only until the answer is in. A failed request leaves the mirrored
  // handle in place, and the post says if it is taken.
  function suggest() {
    if (name.value.trim() === '') {
      handle.value = '';
      return;
    }
    handle.readOnly = true;
    handle.setAttribute('aria-busy', 'true');
    again.disabled = true;
    pending = fetchSuggestion(handle.value).finally(() => {
      pending = null;
      handle.readOnly = false;
      handle.removeAttribute('aria-busy');
      again.disabled = false;
    });
  }

  // before is the handle on the screen when the request was made. The answer
  // is written only over that, so a handle typed in the meantime stays.
  async function fetchSuggestion(before) {
    try {
      const response = await fetch(handle.dataset.suggest + '?name=' + encodeURIComponent(name.value), {
        headers: { Accept: 'application/json' },
      });
      if (response.ok) {
        const body = await response.json();
        if (!byHand && handle.value === before) handle.value = body.handle;
      }
    } catch {
      // Left as mirrored.
    }
  }

  function handleFor(displayName) {
    return displayName
      .toLowerCase()
      .replace(/[^\p{L}\p{N}]+/gu, '_')
      .replace(/^_+/, '')
      .slice(0, 32)
      .replace(/_+$/, '');
  }
})();
