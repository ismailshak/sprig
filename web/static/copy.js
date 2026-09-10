/* The Copy button under a value shown once: an invite link, a sign-in link,
   a new API token or a batch of recovery codes. Writing to the clipboard
   needs a script, so the server renders each button hidden and this shows it
   in a browser with the clipboard API. The value to copy is in the button's
   data-copy attribute. Without a script the value is still on the page as
   text to select. */
(function () {
  if (!navigator.clipboard || typeof navigator.clipboard.writeText !== 'function') return;

  // A token and a sign-in link arrive by an htmx swap, so the buttons in
  // swapped content are shown too. htmx fires load for the whole body once it
  // starts, so a button already shown is skipped.
  show(document);
  document.addEventListener('htmx:load', (event) => show(event.target));

  function show(root) {
    for (const button of root.querySelectorAll('button[data-copy][hidden]')) {
      button.hidden = false;
      const label = button.textContent;
      button.addEventListener('click', async () => {
        try {
          await navigator.clipboard.writeText(button.dataset.copy);
          button.textContent = 'Copied';
        } catch {
          // The browser refused the write, in a window without focus for
          // example. The value is still on the page to select.
          button.textContent = 'Couldn’t copy';
        }
        setTimeout(() => {
          button.textContent = label;
        }, 2000);
      });
    }
  }
})();
