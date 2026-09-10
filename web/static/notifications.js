/* The Notifications page's script. It asks for notification permission and
   subscribes this browser to push, neither of which a form can do. The
   checkboxes and the hour are saved by an ordinary form post. The script sends
   that post itself, after the subscribe.

   The form has data-key, the VAPID public key to subscribe with, and
   data-subscribe, the URL the subscription is posted to. Each checkbox under
   What to send has data-notify. Each Remove form has data-endpoint, the push
   service URL of its row.

   Remove and Send test notification are htmx swaps of the Subscribed devices
   section. The push API work each needs first is done in htmx's confirm
   event. That event holds the request until issueRequest is called. The
   listener is on the document, so it covers the forms a swap brings in. */
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

  document.addEventListener('htmx:confirm', (event) => {
    const form = event.detail.elt;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.dataset.endpoint !== undefined) {
      event.preventDefault();
      unsubscribe(form).finally(() => event.detail.issueRequest(true));
    } else if (form.id === 'push-test') {
      event.preventDefault();
      fillEndpoint(form).finally(() => event.detail.issueRequest(true));
    }
  });

  // unsubscribe takes this browser off the push service when the Remove form
  // is for this browser's own row. Another browser's row needs only the post.
  async function unsubscribe(remove) {
    const subscription = await push.current();
    if (subscription && subscription.endpoint === remove.dataset.endpoint) {
      // A failed unsubscribe is ignored. The row is deleted either way, so
      // nothing will be sent to the subscription again.
      await subscription.unsubscribe().catch(() => {});
    }
  }

  // fillEndpoint puts this browser's push subscription URL in the test form's
  // hidden field, so the server knows which browser to send the test to. Only
  // the push API can read it.
  async function fillEndpoint(test) {
    const subscription = await push.current();
    if (subscription) test.elements.endpoint.value = subscription.endpoint;
  }
})();
