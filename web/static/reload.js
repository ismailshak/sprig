/* Reloads a page that has been hidden for five minutes or more when it is
   shown again. sprig installed on a phone opens onto whatever page it showed
   when it was last used, so care logged from another phone since then is not
   on it until the page is fetched again.

   The page is not reloaded while a field differs from what the server
   rendered, or while a sheet is open, because the reload would lose what was
   typed. It is not reloaded either while a form that posts has a field filled
   in, because a refused post is rendered on the URL it was posted to. A reload
   there sends the post again. Such a page renders the typed values as the
   fields' defaults, so the first check does not catch it. */
(function () {
  const staleAfter = 5 * 60 * 1000;
  // hiddenAt is when the page went hidden, and 0 while it is showing.
  let hiddenAt = 0;

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      hiddenAt = Date.now();
      return;
    }
    const hiddenFor = hiddenAt ? Date.now() - hiddenAt : 0;
    hiddenAt = 0;
    if (hiddenFor < staleAfter) return;
    if (edited() || filledPost() || document.querySelector('dialog[open]')) return;
    location.reload();
  });

  // edited reports whether any field on the page differs from the value the
  // server rendered it with.
  function edited() {
    for (const field of fields(document)) {
      if (field instanceof HTMLSelectElement) {
        if (Array.from(field.options).some((option) => option.selected !== option.defaultSelected)) return true;
      } else if (field.type === 'checkbox' || field.type === 'radio') {
        if (field.checked !== field.defaultChecked) return true;
      } else if (field.type === 'file') {
        if (field.files && field.files.length > 0) return true;
      } else if (field.type !== 'hidden' && field.value !== field.defaultValue) {
        return true;
      }
    }
    return false;
  }

  // filledPost reports whether a form that posts holds a field, other than a
  // hidden one, with a value in it.
  function filledPost() {
    for (const form of document.querySelectorAll('form[method="post" i]')) {
      for (const field of fields(form)) {
        if (field.type !== 'hidden' && field.value !== '') return true;
      }
    }
    return false;
  }

  function fields(root) {
    return root.querySelectorAll('input, textarea, select');
  }
})();
