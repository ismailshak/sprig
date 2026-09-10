/* What htmx does not do itself around a swap: move focus, show a failed swap
   and hold the undo window's request. The layout loads it on every page.

   Focus. A swap that replaces the element holding focus leaves focus on the
   body, and the next Tab starts from the top of the page. htmx puts focus
   back on an element with the focused element's id, and on one marked
   autofocus. For the rest, focus goes to the first control in the element
   the swap brought in: the Undo button on a row just logged, the first field
   of a schedule editor.

   Failures. htmx does not swap an error response, so a failed swap changes
   nothing on the page. The response's own sentence, or one saying sprig could
   not be reached, is written into the live region. The region is fixed to the
   top of the window until the next swap.

   The undo window. A logged row sends a request of its own when its window
   ends. On Today that request removes the row and on Activity it replaces it.
   The stylesheet pauses the row's drain bar while Undo has a focus ring, and
   the request is not sent until the bar has run out, so the bar and the row
   agree on how long is left. */
(function () {
  const status = document.getElementById('status');
  // controls matches every control Tab can reach.
  const controls =
    ':is(a[href], button, input, select, textarea, [tabindex]):not([disabled]):not([tabindex="-1"]):not([type="hidden"])';

  // lost is true when a swap began with an element focused. It is cleared
  // once focus is somewhere again.
  let lost = false;

  document.addEventListener('htmx:beforeSwap', () => {
    const active = document.activeElement;
    lost = active !== null && active !== document.body;
  });

  // afterSettle fires once for each element the swap brought in, the
  // out-of-band ones first. The first that holds a visible control gets the
  // focus.
  document.addEventListener('htmx:afterSettle', (event) => {
    if (!lost) return;
    const active = document.activeElement;
    if (active && active !== document.body && active.isConnected) {
      lost = false;
      return;
    }
    const first = firstControl(event.target);
    if (!first) return;
    first.focus();
    lost = false;
  });

  function firstControl(root) {
    if (!(root instanceof Element)) return null;
    const candidates = root.matches(controls) ? [root] : root.querySelectorAll(controls);
    for (const control of candidates) {
      if (control.getClientRects().length > 0) return control;
    }
    return null;
  }

  document.addEventListener('htmx:responseError', (event) => {
    const text = event.detail.xhr.responseText;
    say(text && !text.startsWith('<') ? text : 'Something went wrong. Try again.');
  });
  document.addEventListener('htmx:sendError', () => {
    say('Couldn’t reach sprig. Check the connection and try again.');
  });
  document.addEventListener('htmx:afterSwap', () => {
    status.classList.remove('status--shown');
  });

  function say(text) {
    status.textContent = text;
    status.classList.add('status--shown');
  }

  // htmx's delay fires once and cannot pause, so when it fires the request
  // waits for the drain bar's animation to finish. A pause on the bar delays
  // the request by the same amount. Pressing Undo swaps the row out, which
  // cancels the animation, and the request is never sent.
  document.addEventListener('htmx:confirm', (event) => {
    const { elt, triggeringEvent, issueRequest } = event.detail;
    if (triggeringEvent || !elt.classList.contains('row--done')) return;
    const bar = elt.getAnimations({ subtree: true }).find((a) => a.animationName === 'grace-drain');
    if (!bar || bar.playState === 'finished') return;
    event.preventDefault();
    bar.finished.then(
      () => issueRequest(true),
      () => {},
    );
  });
})();
