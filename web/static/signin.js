/* Removes the copies of Activity and the calendar that htmx keeps in this
   tab's sessionStorage for Back and Forward. The sign-in page loads it so the
   copies are gone once the session ends. Signing out, closing the account and
   an expired session all end on that page. */
(function () {
  try {
    sessionStorage.removeItem('htmx-history-cache');
  } catch {
    // Storage is refused. htmx could not have written the copies either.
  }
})();
