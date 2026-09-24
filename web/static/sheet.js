/* The log-care sheet, the garden sheet on Today and the day sheet on the
   calendar. The server renders each with the open attribute. That is a
   non-modal dialog, and the page behind it stays in the tab order. This script
   reopens it as a modal, so Tab stays inside the sheet, Escape closes it and
   focus goes back to the control that opened it.

   A sheet can be dragged down to close it, the same as Cancel or Close. The
   log-care sheet is dragged by its grip or its plant heading. The garden sheet
   and the day sheet are dragged by their grip or their title.

   A sheet is swapped into the page by htmx, so the listeners are on the
   document and find the sheet from the event. A drag that starts in the
   panel's body is ignored, because that is how the panel scrolls. */
(function () {
  // modals is the dialogs already reopened as modals. Reopening one a second
  // time would close it.
  const modals = new WeakSet();
  // shown is the sheet most recently reopened as a modal. A swap that removes
  // it and adds no other sheet is the response to a save or a delete.
  let shown = null;
  // opener is the element that had focus when the sheet was requested, such as
  // a row's link or Log care. Focus goes back to it when the sheet closes.
  // When nothing had focus it is the element that sent the request, because
  // Safari does not focus a link or button on a click or a tap. The browser's
  // own record of the opener is not used, because WebKit has already focused
  // the dialog when showModal records it.
  let opener = null;

  document.addEventListener('htmx:beforeSwap', (event) => {
    if (event.detail.target.id !== 'sheet') return;
    const active = document.activeElement;
    // A refused time swaps the open sheet for another while focus is inside
    // it. The opener stays the element that opened the first one.
    if (active && active.closest('#sheet')) return;
    opener = active && active !== document.body ? active : event.detail.requestConfig.elt;
  });

  // modal reopens an open dialog as a modal. The open attribute has to come
  // off first, because showModal refuses a dialog that is already open. The
  // dialog itself is then focused, so the screen reader reads its label and
  // then the panel from the top. Chrome would otherwise focus the scrim.
  const modal = (dialog) => {
    if (!dialog.open || modals.has(dialog)) return;
    modals.add(dialog);
    shown = dialog;
    dialog.removeAttribute('open');
    dialog.showModal();
    dialog.focus();
    dialog.addEventListener('close', () => {
      if (opener && opener.isConnected) opener.focus();
      opener = null;
    });
  };

  // refocus puts focus back on the opener, without scrolling, after a save or
  // delete removed the sheet. When the swap replaced the opener, focus goes to
  // the first link or button in the element that now has the id of the
  // opener's nearest ancestor with an id. When a correction moved the event
  // off the log being shown, it goes to the first link or button in the
  // element with swappedID. Without this,
  // focus goes to the top row of the log and the page scrolls up to it.
  const refocus = (swappedID) => {
    if (!opener) return;
    let target = opener;
    if (!opener.isConnected) {
      const row = opener.closest('[id]');
      const replaced = (row && document.getElementById(row.id)) || document.getElementById(swappedID);
      target = replaced && replaced.querySelector('a[href], button');
    }
    opener = null;
    if (target) target.focus({ preventScroll: true });
  };

  // A sheet rendered with the page is made modal at once. One swapped in
  // later is found after the swap, because htmx:load reports the element it
  // added and an outerHTML swap of #sheet can report the parent instead.
  const sheetOnPage = () => document.querySelector('dialog.sheet[open]');
  const open = sheetOnPage();
  if (open) modal(open);
  // htmx fires afterSwap on each element a swap adds, once all of them are in
  // place. detail.target is the element the request targeted. An outerHTML
  // swap has already removed it, so refocus looks its id up again.
  document.addEventListener('htmx:afterSwap', (event) => {
    const dialog = sheetOnPage();
    if (dialog) {
      modal(dialog);
    } else if (shown && !shown.isConnected) {
      shown = null;
      refocus(event.detail.target.id);
    }
  });

  // closeAfter is how many pixels the panel has to be dragged down for the
  // release to close it. A shorter drag puts the panel back.
  const closeAfter = 80;
  // tapUnder is how many pixels the pointer has to move down before a press is
  // a drag. A shorter press on the plant heading is a tap on its link.
  const tapUnder = 8;
  // drag is the drag in progress. Null between drags.
  let drag = null;
  // dragged is the element a drag was just released from, so the click the
  // release fires can be cancelled. Null otherwise.
  let dragged = null;

  document.addEventListener('pointerdown', (event) => {
    const handle = event.target.closest('.sheet__grip, .sheet__plant, .sheet__title');
    if (!handle) return;
    // A mouse press on the plant heading or the title is ignored, because
    // above 900px the sheet is a centred dialog and a mouse drag selects text.
    if (!handle.classList.contains('sheet__grip') && event.pointerType === 'mouse') return;
    const panel = handle.closest('.sheet__panel');
    if (!panel) return;
    handle.setPointerCapture(event.pointerId);
    drag = { from: event.clientY, panel, handle, moved: 0 };
  });

  document.addEventListener('pointermove', (event) => {
    if (!drag) return;
    drag.moved = Math.max(0, event.clientY - drag.from);
    if (drag.moved < tapUnder) return;
    drag.panel.style.transition = 'none';
    drag.panel.style.transform = 'translateY(' + drag.moved + 'px)';
  });

  const release = () => {
    if (!drag) return;
    const { panel, handle, moved } = drag;
    drag = null;
    panel.style.transition = '';
    panel.style.transform = '';
    if (moved < tapUnder) return;
    dragged = handle;
    // The click follows the release in the same task, so the mark is cleared
    // straight after it.
    setTimeout(() => (dragged = null));
    if (moved < closeAfter) return;
    const dialog = panel.closest('dialog');
    if (dialog) dialog.close();
  };
  document.addEventListener('pointerup', release);
  document.addEventListener('pointercancel', release);

  document.addEventListener(
    'click',
    (event) => {
      if (!dragged || !dragged.contains(event.target)) return;
      event.preventDefault();
      event.stopPropagation();
    },
    true,
  );
})();
