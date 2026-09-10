/* The Remind me again banner's script. The banner is the element with id
   remind-again.

   Removes the query parameter named by data-param from the URL once the page
   has loaded. That parameter is what makes the server render the banner, so
   a reload would otherwise show it again.

   Dismiss is a link to Today. A click on it hides the banner instead of
   following the link. The listener is on the document because a swap
   replaces the banner and its link. */
(function () {
  const banner = document.getElementById('remind-again');
  if (!banner) return;
  const url = new URL(location.href);
  if (url.searchParams.has(banner.dataset.param)) {
    url.searchParams.delete(banner.dataset.param);
    history.replaceState(history.state, '', url);
  }

  document.addEventListener('click', (event) => {
    const link = event.target.closest('#remind-again a[data-dismiss]');
    if (!link) return;
    event.preventDefault();
    link.closest('#remind-again').hidden = true;
  });
})();
