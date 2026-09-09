/* Adds the photo--loaded class to each photo once it has loaded or failed. On
   a page whose root has the js attribute the stylesheet hides a photo until
   then and shows a spinner over its box. A browser paints a JPEG top down as
   its bytes arrive, so without this a half-drawn photo shows instead. Photos
   the lazy grid swaps in are covered through htmx's load event. */
(function () {
  function watch(root) {
    for (const img of root.querySelectorAll('img[data-photo]')) {
      const done = () => img.classList.add('photo--loaded');
      // complete is also true for an image that failed to load, so the decoded
      // width says whether there is a picture to show.
      if (img.complete && img.naturalWidth > 0) {
        done();
        continue;
      }
      img.addEventListener('load', done, { once: true });
      img.addEventListener('error', done, { once: true });
    }
  }
  watch(document);
  document.addEventListener('htmx:load', (event) => watch(event.target));
})();
