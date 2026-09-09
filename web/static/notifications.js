/* The Notifications page's script. It asks for notification permission and
   subscribes this browser to push, neither of which a form can do. The
   checkboxes and the hour are saved by an ordinary form post. The script sends
   that post itself, after the subscribe.

   The form has data-key, the VAPID public key to subscribe with, and
   data-subscribe, the URL the subscription is posted to. Each checkbox under
   What to send has data-notify. Each Remove form has data-endpoint, the push
   service URL of its row. */
(function () {
  const form = document.getElementById('push-form');
  if (!form || !window.sprigPush) return;
  const push = window.sprigPush;
  const unavailable = document.getElementById('push-unavailable');
  const message = document.getElementById('push-error');

  // On an iPhone the push API only exists once sprig is on the Home Screen.
  // Without it the form is hidden and the Install sprig block is shown.
  if (!push.supported) {
    form.hidden = true;
    unavailable.hidden = false;
    return;
  }

  // Checking a box asks for permission, because a browser only opens the
  // prompt during a click. A refusal is written into the alert line under the
  // checkboxes.
  for (const box of form.querySelectorAll('input[data-notify]')) {
    box.addEventListener('change', async () => {
      if (!box.checked) return;
      message.textContent = '';
      const permission = await Notification.requestPermission();
      if (permission !== 'granted') {
        message.textContent =
          'Notifications are blocked on this device. Allow them in your browser’s settings, then try again.';
      }
    });
  }

  // Subscribing on submit, rather than when a box is checked, also covers a
  // browser whose boxes were already checked on another device. The form is
  // posted whether or not the subscribe worked, because the checkboxes and the
  // hour belong to the account and not to this browser.
  let saving = false;
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    if (saving) return;
    saving = true;
    if (form.querySelector('input[data-notify]:checked')) {
      try {
        await push.subscribe(form.dataset.key, form.dataset.subscribe);
      } catch {
        // Nothing is shown, because the form post below replaces the page.
      }
    }
    form.submit();
  });

  // Remove on this browser's own row unsubscribes it from the push service
  // before the post deletes the row. Another browser's row is only the post.
  for (const remove of document.querySelectorAll('form[data-endpoint]')) {
    remove.addEventListener('submit', async (event) => {
      event.preventDefault();
      const subscription = await push.current();
      if (subscription && subscription.endpoint === remove.dataset.endpoint) {
        // A failed unsubscribe is ignored. The row is deleted either way, so
        // nothing will be sent to the subscription again.
        await subscription.unsubscribe().catch(() => {});
      }
      remove.submit();
    });
  }

  // The hidden endpoint field tells the server which browser to send the test
  // to. Only the push API can read it, so the script fills the field in before
  // the form posts.
  const test = document.getElementById('push-test');
  if (test) {
    test.addEventListener('submit', async (event) => {
      event.preventDefault();
      const subscription = await push.current();
      if (subscription) test.elements.endpoint.value = subscription.endpoint;
      test.submit();
    });
  }
})();
