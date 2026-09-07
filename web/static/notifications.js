/* The Notifications page's script. It asks for notification permission and
   subscribes this browser to push, neither of which a form can do. Saving the
   types is the form's own post.

   The form has data-key, the VAPID public key to subscribe with, and
   data-subscribe, the URL the subscription is posted to. Each type's checkbox
   has data-notify. Each Remove form has data-endpoint, the push service URL of
   its row. */
(function () {
  const form = document.getElementById('push-form');
  if (!form) return;
  const unavailable = document.getElementById('push-unavailable');
  const message = document.getElementById('push-error');

  // On an iPhone the push API only exists once sprig is on the Home Screen.
  // Without it the form is hidden and the Install sprig block is shown.
  if (!('PushManager' in window) || !('serviceWorker' in navigator)) {
    form.hidden = true;
    unavailable.hidden = false;
    return;
  }

  for (const box of form.querySelectorAll('input[data-notify]')) {
    box.addEventListener('change', async () => {
      if (!box.checked) return;
      message.textContent = '';
      try {
        // The prompt only opens inside the tap that asked for it, so it comes
        // before the first await.
        const permission = await Notification.requestPermission();
        if (permission !== 'granted') {
          message.textContent =
            'This browser gave no permission, so nothing will arrive here. If you blocked notifications for sprig, allow them in the browser’s settings and turn a type on again.';
          return;
        }
        const registration = await navigator.serviceWorker.ready;
        const subscription = await registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: decode(form.dataset.key),
        });
        const { endpoint, keys } = subscription.toJSON();
        const response = await fetch(form.dataset.subscribe, {
          method: 'POST',
          body: new URLSearchParams({ endpoint, p256dh: keys.p256dh, auth: keys.auth }),
        });
        if (!response.ok) throw new Error('the subscription was refused with ' + response.status);
      } catch {
        message.textContent = 'This browser could not be subscribed. Try again.';
      }
    });
  }

  // Remove on this browser's own row unsubscribes it from the push service
  // before the post deletes the row. Another browser's row is only the post.
  for (const remove of document.querySelectorAll('form[data-endpoint]')) {
    remove.addEventListener('submit', async (event) => {
      event.preventDefault();
      try {
        const registration = await navigator.serviceWorker.getRegistration();
        const subscription = registration && (await registration.pushManager.getSubscription());
        if (subscription && subscription.endpoint === remove.dataset.endpoint) await subscription.unsubscribe();
      } catch {
        // The row is deleted either way. A browser that cannot unsubscribe
        // holds a subscription nothing will be sent to.
      }
      remove.submit();
    });
  }

  // decode turns the base64url key into the bytes the push API takes. Safari
  // does not accept the string form.
  function decode(key) {
    const binary = atob(key.replace(/-/g, '+').replace(/_/g, '/'));
    return Uint8Array.from(binary, (char) => char.charCodeAt(0));
  }
})();
