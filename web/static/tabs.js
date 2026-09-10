/* A press on the tab for the page being shown scrolls the page to the top. It
   reloads the page only when the page is already at the top, so the press
   that refreshes an installed app is kept. The tabs are the sidebar above
   900px, so one listener covers both.

   The scroll is animated here rather than through scrollTo's smooth option,
   because iOS Safari ignores that option on an overflow container and jumps.
   Animating it keeps the instant scrollTop the Activity page sets to restore
   its position on Back, which a CSS scroll-behavior would turn into a slide. */
(function () {
  const main = document.getElementById('main');
  // The scroll takes this many milliseconds.
  const duration = 280;

  document.addEventListener('click', (event) => {
    // A click with a modifier, or from any button but the left one, opens the
    // link in a new tab or window and is left alone.
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const tab = event.target.closest('.nav__item[aria-current="page"]');
    if (!tab || main.scrollTop === 0) return;
    event.preventDefault();
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      main.scrollTop = 0;
      return;
    }
    animateToTop();
  });

  function animateToTop() {
    const from = main.scrollTop;
    const start = performance.now();
    const step = (now) => {
      const t = Math.min(1, (now - start) / duration);
      // easeOutCubic, so it slows as it arrives.
      main.scrollTop = from * (1 - (1 - t) ** 3);
      if (t < 1) requestAnimationFrame(step);
    };
    requestAnimationFrame(step);
  }
})();
