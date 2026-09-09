/* Subscribing this browser to push notifications. The Notifications page, the
   Reminders page and the banner on Today each do it from one press. This file
   holds what they share. It is loaded before the page's own script, which
   reads window.sprigPush. */
window.sprigPush = (function () {
  // supported is whether this browser has the push API. On an iPhone it only
  // exists once sprig is on the Home Screen.
  const supported = 'PushManager' in window && 'serviceWorker' in navigator;

  // subscribeWait is how long to wait on pushManager.subscribe before giving
  // up. The call never settles when the push service cannot be reached.
  const subscribeWait = 10_000;

  // subscribe asks for notification permission, subscribes this browser with
  // the VAPID public key and posts the subscription to url. It returns the
  // permission: 'granted' once the browser is subscribed, and 'denied' or
  // 'default' when it was not given, with nothing subscribed. It throws when
  // the push service does not answer or the post is refused. A browser only
  // opens the permission prompt during a click, so it is called from one.
  // Calling it for a browser that is already subscribed is safe, because
  // pushManager.subscribe returns the subscription it has.
  async function subscribe(key, url) {
    const permission = await Notification.requestPermission();
    if (permission !== 'granted') return permission;
    const registration = await navigator.serviceWorker.ready;
    const subscription = await Promise.race([
      registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: decode(key),
      }),
      new Promise((_, reject) => setTimeout(() => reject(new Error('the push service did not answer')), subscribeWait)),
    ]);
    const { endpoint, keys } = subscription.toJSON();
    const response = await fetch(url, {
      method: 'POST',
      body: new URLSearchParams({
        endpoint,
        p256dh: keys.p256dh,
        auth: keys.auth,
      }),
    });
    if (!response.ok) throw new Error('the subscription was refused with ' + response.status);
    return permission;
  }

  // current returns this browser's push subscription, or null when it has
  // none and when the push API throws.
  async function current() {
    try {
      const registration = await navigator.serviceWorker.getRegistration();
      return (registration && (await registration.pushManager.getSubscription())) || null;
    } catch {
      return null;
    }
  }

  // decode turns the base64url key into the bytes the push API takes. Safari
  // does not accept the string form.
  function decode(key) {
    const binary = atob(key.replace(/-/g, '+').replace(/_/g, '/'));
    return Uint8Array.from(binary, (char) => char.charCodeAt(0));
  }

  return { supported, subscribe, current };
})();
