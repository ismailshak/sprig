/* Selects the browser's own timezone on a Timezone select the server marked
   with data-propose, so a new account has one zone to check rather than four
   hundred to search. The server marks the select only on a form that has not
   been posted, so a zone somebody chose is never overwritten. */
(function () {
  const select = document.querySelector('select[name="timezone"][data-propose]');
  if (!select) return;

  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  // Chrome and Safari report some zones under the name ICU gives them, such as
  // Asia/Calcutta for Asia/Kolkata. The option for such a zone holds the ICU
  // name in data-also.
  const option = Array.from(select.options).find((o) => o.value === zone || o.dataset.also === zone);
  if (option) option.selected = true;
})();
