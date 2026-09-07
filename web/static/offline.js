/* Registers the service worker. When the worker says it served this page from
   its cache, this shows the line at the top of the page and fills the time in
   from the Date header of the cached copy. */
(function () {
  if (!('serviceWorker' in navigator)) return;

  navigator.serviceWorker.register('/service-worker.js');

  navigator.serviceWorker.addEventListener('message', (event) => {
    if (!event.data || event.data.type !== 'offline') return;
    const line = document.getElementById('offline');
    const time = line.querySelector('time');
    const fetchedAt = new Date(event.data.fetchedAt);
    time.dateTime = fetchedAt.toISOString();
    time.textContent = whenFetched(fetchedAt);
    line.hidden = false;
  });

  // The worker posts while the page is still loading, before this listener
  // exists. The browser holds those messages until startMessages is called.
  navigator.serviceWorker.startMessages();

  // whenFetched returns "at 14:32" for a copy fetched today and "on 3 Sept at
  // 14:32" for an older one, in the browser's own language and timezone.
  function whenFetched(fetchedAt) {
    const clock = fetchedAt.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    if (fetchedAt.toDateString() === new Date().toDateString()) return 'at ' + clock;
    const day = fetchedAt.toLocaleDateString([], { day: 'numeric', month: 'short' });
    return 'on ' + day + ' at ' + clock;
  }
})();
