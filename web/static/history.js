/* Keeps the scroll position across Back and Forward on a page where htmx
   handles them. htmx saves and restores the window's scroll, but the page
   scrolls the main column, so without this Back lands at the top. */
(function () {
  const main = document.getElementById('main');
  // scrollTops is the main column's scrollTop for each URL htmx has left,
  // keyed by path.
  const scrollTops = new Map();

  document.addEventListener('htmx:beforeHistorySave', (event) => {
    scrollTops.set(event.detail.path, main.scrollTop);
  });

  document.addEventListener('htmx:historyRestore', (event) => {
    const top = scrollTops.get(event.detail.path);
    if (top !== undefined) main.scrollTop = top;
  });
})();
