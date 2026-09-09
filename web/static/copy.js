/* The Copy button under a value shown once: an invite link, a sign-in link,
   a new API token or a batch of recovery codes. Writing to the clipboard
   needs a script, so the server renders each button hidden and this shows it
   in a browser with the clipboard API. The value to copy is in the button's
   data-copy attribute. Without a script the value is still on the page as
   text to select. */
(function () {
  const buttons = document.querySelectorAll('button[data-copy]');
  if (buttons.length === 0 || !navigator.clipboard || typeof navigator.clipboard.writeText !== 'function') return;

  for (const button of buttons) {
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
})();
