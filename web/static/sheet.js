/* The log-care sheet and the garden sheet. The server renders each with the
   open attribute. That is a non-modal dialog, and the page behind it stays in
   the tab order. This script reopens it as a modal, so Tab stays inside
   the sheet, Escape closes it and focus goes back to the control that opened
   it. It also lets the sheet be dragged down to close, by its grip or its
   plant heading, the same as Cancel.

   A sheet is swapped into the page by htmx, so the listeners are on the
   document and find the sheet from the event. Dragging from the panel's body
   is left alone, because that is how the panel scrolls. */
(function () {
  // modals is the dialogs already reopened as modals. Reopening one a second
  // time would close it.
  const modals = new WeakSet();
  // opener is the element that had focus when the sheet was requested, the
  // row's link or the Log care button. It is null when nothing had focus, and
  // focus goes back to it when the sheet closes. The browser records an opener
  // of its own at showModal, but WebKit has focused the dialog by then, so the
  // browser's record is the dialog.
  let opener = null;

  document.addEventListener('htmx:beforeSwap', (event) => {
    if (event.detail.target.id !== 'sheet') return;
    const active = document.activeElement;
    opener = active && active !== document.body ? active : null;
  });

  // modal reopens an open dialog as a modal. The open attribute has to come
  // off first, because showModal refuses a dialog that is already open. The
  // dialog itself is then focused, so the screen reader reads its label and
  // then the panel from the top. Chrome would otherwise focus the scrim.
  const modal = (dialog) => {
    if (!dialog.open || modals.has(dialog)) return;
    modals.add(dialog);
    dialog.removeAttribute('open');
    dialog.showModal();
    dialog.focus();
    dialog.addEventListener('close', () => {
      if (opener && opener.isConnected) opener.focus();
      opener = null;
    });
  };

  // A sheet rendered with the page is made modal at once. One swapped in
  // later is found after the swap, because htmx:load reports the element it
  // added and an outerHTML swap of #sheet can report the parent instead.
  const sheetOnPage = () => document.querySelector('dialog.sheet[open]');
  const open = sheetOnPage();
  if (open) modal(open);
  document.addEventListener('htmx:afterSwap', () => {
    const dialog = sheetOnPage();
    if (dialog) modal(dialog);
  });

  // closeAfter is how many pixels the panel has to be dragged down for the
  // release to close it. A shorter drag puts the panel back.
  const closeAfter = 80;
  // tapUnder is the movement in pixels under which a press and release on the
  // heading is a tap on its link. From there on it is a drag, and the link's
  // click is cancelled.
  const tapUnder = 8;
  // drag is the drag in progress: the pointer's starting y, the panel being
  // moved and the grip or heading it is held by. Null between drags.
  let drag = null;
  // dragged is the heading a drag was just released from, so the click the
  // release fires can be cancelled. Null otherwise.
  let dragged = null;

  document.addEventListener('pointerdown', (event) => {
    const handle = event.target.closest('.sheet__grip, .sheet__plant');
    if (!handle) return;
    // Above 900px the sheet is a centred dialog, where a mouse drag on the
    // heading is a text selection.
    if (handle.classList.contains('sheet__plant') && event.pointerType === 'mouse') return;
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
