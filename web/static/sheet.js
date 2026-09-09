/* Dragging a sheet down by its grip closes it, the same as Cancel. The grip
   is the short bar at the top of the panel.

   The sheet is swapped into the page by htmx, so the listeners are on the
   document and find the sheet from the event. Dragging from the panel's body
   is left alone, because that is how the panel scrolls. */
(function () {
  // closeAfter is how many pixels the panel has to be dragged down for the
  // release to close it. A shorter drag puts the panel back.
  const closeAfter = 80;
  // drag is the drag in progress: the pointer's starting y and the panel being
  // moved. Null between drags.
  let drag = null;

  document.addEventListener('pointerdown', (event) => {
    const grip = event.target.closest('.sheet__grip');
    if (!grip) return;
    const panel = grip.closest('.sheet__panel');
    if (!panel) return;
    grip.setPointerCapture(event.pointerId);
    drag = { from: event.clientY, panel, moved: 0 };
    panel.style.transition = 'none';
    event.preventDefault();
  });

  document.addEventListener('pointermove', (event) => {
    if (!drag) return;
    drag.moved = Math.max(0, event.clientY - drag.from);
    drag.panel.style.transform = 'translateY(' + drag.moved + 'px)';
  });

  const release = () => {
    if (!drag) return;
    const { panel, moved } = drag;
    drag = null;
    panel.style.transition = '';
    panel.style.transform = '';
    if (moved < closeAfter) return;
    const dialog = panel.closest('dialog');
    if (dialog) dialog.close();
  };
  document.addEventListener('pointerup', release);
  document.addEventListener('pointercancel', release);
})();
