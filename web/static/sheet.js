/* The log-care sheet, the garden sheet and the Remind me later sheet on
   Today, and the day sheet and the note sheet on the calendar. The server
   renders each with the open attribute. That is a non-modal dialog, and the
   page behind it stays in the tab order. This script reopens it as a modal,
   so Tab stays inside the sheet, Escape closes it and focus goes back to the
   control that opened it.

   A finger dragged down anywhere on a sheet's panel closes the sheet, the
   same as Cancel or Close. While the panel is scrolled down, the finger
   scrolls it. The panel moves only once it is scrolled to the top. A mouse
   does not drag a sheet.

   A sheet is swapped into the page by htmx, so the htmx and click listeners
   are on the document and find the sheet from the event. The touch listeners
   are added to each sheet's dialog when it is made modal, because a
   non-passive touchmove listener on the document would make every scroll on
   the page wait for this script. */
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
    // Add note and a note on the calendar's day sheet swap that sheet for the
    // note sheet while focus is inside it. The opener stays the day's link in
    // the grid that opened the day sheet.
    if (active && active.closest('#sheet')) return;
    opener = active && active !== document.body ? active : event.detail.requestConfig.elt;
  });

  // closeAfter is how many pixels the panel has to be dragged down for the
  // release to close it. A shorter drag puts the panel back.
  const closeAfter = 80;
  // tapUnder is how many pixels the finger has to move down before a touch is
  // a drag. A shorter touch on a link or button is a tap on it.
  const tapUnder = 8;
  // drag is the touch in progress on a sheet's panel, or null. y is the
  // finger's clientY at the last touch event. from is the clientY where the
  // panel started to follow the finger, or null before that. moved is how many
  // pixels the finger is below from.
  let drag = null;
  // dragged is the element the last drag started on, or null. The click the
  // browser fires after the drag is cancelled.
  let dragged = null;

  // putBack clears the transform and transition a drag set on the panel.
  const putBack = (panel) => {
    panel.style.transition = '';
    panel.style.transform = '';
  };

  // A one-finger touch inside a sheet's panel starts a drag. A second finger
  // puts the panel back and ends the drag.
  const touchStart = (event) => {
    if (drag) putBack(drag.panel);
    const panel = event.touches.length === 1 && event.target.closest('.sheet__panel');
    drag = panel ? { panel, target: event.target, y: event.touches[0].clientY, from: null, moved: 0 } : null;
  };

  // The browser scrolls the panel until the finger moves down while the panel
  // is scrolled to the top. Every move after that is cancelled and moves the
  // panel with the finger, including a move back up. The listener is not
  // passive, because a passive listener cannot cancel a move.
  const touchMove = (event) => {
    if (!drag) return;
    const y = event.touches[0].clientY;
    if (drag.from === null && y > drag.y && drag.panel.scrollTop <= 0) drag.from = drag.y;
    drag.y = y;
    if (drag.from === null) return;
    event.preventDefault();
    drag.moved = Math.max(0, y - drag.from);
    drag.panel.style.transition = 'none';
    drag.panel.style.transform = 'translateY(' + drag.moved + 'px)';
  };

  const release = () => {
    if (!drag) return;
    const { panel, target, moved } = drag;
    drag = null;
    putBack(panel);
    if (moved < tapUnder) return;
    dragged = target;
    // The click is fired in the same task as the touchend, so dragged is
    // cleared in the next task.
    setTimeout(() => (dragged = null));
    if (moved < closeAfter) return;
    const dialog = panel.closest('dialog');
    if (dialog) dialog.close();
  };

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
    dialog.addEventListener('touchstart', touchStart);
    dialog.addEventListener('touchmove', touchMove, { passive: false });
    dialog.addEventListener('touchend', release);
    dialog.addEventListener('touchcancel', release);
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
