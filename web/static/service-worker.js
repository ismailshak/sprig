/* The service worker, served at /service-worker.js with VERSION, SHELL,
   OFFLINE, SIGN_IN, GARDENS and ICON declared above it. The copy under
   /static/ has none of them and is not registered, because a worker's scope
   is the directory it was loaded from.

   The shell cache is filled once at install from SHELL. The pages cache holds
   a copy of every page a navigation fetched, so a page can still be shown when
   the network is gone. VERSION is a hash of the shell files and the templates.
   Both cache names include it, so a new worker starts with empty caches. */

const SHELL_CACHE = 'shell-' + VERSION;
const PAGES_CACHE = 'pages-' + VERSION;

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(SHELL_CACHE).then((cache) => cache.addAll(SHELL)));
  // Take over from the old worker without waiting for its tabs to close.
  // Activation waits for the precache above, so no page this worker serves is
  // missing a file.
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((names) =>
        Promise.all(
          names.filter((name) => name !== SHELL_CACHE && name !== PAGES_CACHE).map((name) => caches.delete(name)),
        ),
      )
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin) return;
  if (event.request.mode === 'navigate') {
    event.respondWith(page(event));
  } else if (SHELL.includes(url.pathname)) {
    // The cache name includes VERSION, so a hit here is never stale.
    event.respondWith(
      caches.open(SHELL_CACHE).then((cache) => cache.match(url.pathname).then((hit) => hit || fetch(event.request))),
    );
  }
});

// page returns the response to a navigation: the server's when it can be
// reached, and otherwise the cached copy of the page or the offline page.
async function page(event) {
  const { request } = event;
  let response;
  try {
    response = await fetch(request);
  } catch {
    return fromCache(event);
  }
  // A navigation's redirect is opaque to fetch(), so a garden switch is
  // recognised by the URL the request posted to. Every cached page was fetched
  // for the garden being switched away from.
  if (request.method !== 'GET' && new URL(request.url).pathname === GARDENS) {
    await caches.delete(PAGES_CACHE);
    return response;
  }
  if (!response.ok) return response;
  if (new URL(response.url).pathname === SIGN_IN) {
    // The sign-in page means the session is over, by signing out or by
    // expiry. Nothing fetched under it should stay on the device.
    await caches.delete(PAGES_CACHE);
  } else if (
    request.method === 'GET' &&
    !response.redirected &&
    (response.headers.get('Content-Type') || '').startsWith('text/html')
  ) {
    // The copy is stored after the response is handed to the browser, so the
    // page is not held back while its body is read into the cache. Until the
    // page arrives the browser shows the address in place of a title.
    const copy = response.clone();
    event.waitUntil(caches.open(PAGES_CACHE).then((pages) => pages.put(request.url, copy)));
  }
  return response;
}

async function fromCache(event) {
  const { request } = event;
  if (request.method === 'GET') {
    const pages = await caches.open(PAGES_CACHE);
    const hit = await pages.match(request.url);
    if (hit) {
      event.waitUntil(postFetchedAt(event.resultingClientId, hit.headers.get('Date')));
      return hit;
    }
  }
  const shell = await caches.open(SHELL_CACHE);
  return shell.match(OFFLINE);
}

// postFetchedAt sends the client the Date header of the cached copy, so the
// page can say when it was fetched. The client does not exist until the
// browser has committed the response, so this polls for it.
async function postFetchedAt(clientId, fetchedAt) {
  for (let attempt = 0; attempt < 50; attempt++) {
    const client = await self.clients.get(clientId);
    if (client) {
      client.postMessage({ type: 'offline', fetchedAt });
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
}

// A push message is JSON with a title, a body, the URL to open and an icon
// URL. The icon is the plant's picture when the notification is about one
// plant, and empty otherwise.
self.addEventListener('push', (event) => {
  const notification = event.data ? event.data.json() : {};
  event.waitUntil(
    self.registration.showNotification(notification.title || 'sprig', {
      body: notification.body,
      icon: notification.icon || ICON,
      data: { url: notification.url },
    }),
  );
});

// Pressing the notification navigates a window sprig already has open to the
// URL, or opens a new one.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const url = event.notification.data && event.notification.data.url;
  if (!url) return;
  event.waitUntil(
    self.clients.matchAll({ type: 'window' }).then(async (windows) => {
      if (windows.length > 0) {
        const focused = await windows[0].focus();
        await focused.navigate(url);
        return;
      }
      await self.clients.openWindow(url);
    }),
  );
});
