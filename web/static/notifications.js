/* The Notifications page's script. Add this device subscribes this browser to
   push. After a save, the Saved beside the control that changed is shown for
   two seconds.

   The Add this device form has data-key, the VAPID public key to subscribe
   with, and hidden fields for the subscription. Each Remove form has
   data-endpoint, the push service URL of its row.

   Add this device, Remove and Send test notification swap the Subscribed
   devices section with htmx. Each calls the push API before its request is
   sent, so the script cancels htmx's confirm event and calls issueRequest when
   the push API has returned. The listeners are on the document so they cover
   the forms a swap adds. */
(function () {
  const form = document.getElementById('push-form');
  if (!form || !window.sprigPush) return;
  const push = window.sprigPush;

  // On an iPhone the push API only exists once sprig is on the Home Screen.
  // Without it the form is hidden and the Install sprig block is shown. Add
  // this device stays hidden.
  if (!push.supported) {
    form.hidden = true;
    document.getElementById('push-unavailable').hidden = false;
    return;
  }

  // shown is the Saved beside a control that is on screen, or null. hide is
  // the timer that hides it. A second save hides the first one's Saved at
  // once, because the hour's save swaps nothing and would leave it showing.
  let shown = null;
  let hide = 0;
  document.addEventListener('htmx:afterRequest', (event) => {
    const { successful, requestConfig } = event.detail;
    const control = requestConfig.triggeringEvent && requestConfig.triggeringEvent.target;
    if (!successful || !(control instanceof Element) || !control.id) return;
    const saved = document.getElementById(control.id + '-saved');
    if (!saved) return;
    clearTimeout(hide);
    if (shown) shown.hidden = true;
    shown = saved;
    saved.hidden = false;
    hide = setTimeout(() => (saved.hidden = true), 2000);
  });

  // endpoint is this browser's push subscription URL, null when it has none
  // and undefined until the push API returns. It is read once and kept,
  // because reading it again after each swap would paint a frame with Add this
  // device hidden.
  let endpoint;
  push.current().then((subscription) => {
    endpoint = subscription ? subscription.endpoint : null;
    showAdd();
  });
  document.addEventListener('htmx:afterSwap', showAdd);

  document.addEventListener('htmx:confirm', (event) => {
    const form = event.detail.elt;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.id === 'push-add') {
      event.preventDefault();
      fillSubscription(form).then((filled) => {
        if (filled) event.detail.issueRequest(true);
      });
    } else if (form.dataset.endpoint !== undefined) {
      event.preventDefault();
      unsubscribe(form).finally(() => event.detail.issueRequest(true));
    } else if (form.id === 'push-test') {
      event.preventDefault();
      fillEndpoint(form).finally(() => event.detail.issueRequest(true));
    }
  });

  // showAdd shows Add this device unless this browser's push subscription is
  // one of the rows under Subscribed devices.
  function showAdd() {
    const add = document.getElementById('push-add');
    if (!add || endpoint === undefined) return;
    const rows = Array.from(document.querySelectorAll('#devices form[data-endpoint]'));
    add.hidden = rows.some((row) => row.dataset.endpoint === endpoint);
  }

  // fillSubscription subscribes this browser and writes the subscription into
  // the Add this device form's hidden fields. When permission is refused or the
  // push service does not respond, it writes the reason into the alert line
  // and returns false.
  async function fillSubscription(add) {
    const message = document.getElementById('push-error');
    message.textContent = '';
    let subscription;
    try {
      subscription = await push.subscription(add.dataset.key);
    } catch {
      message.textContent = 'This device couldn’t be added. Try again in a minute.';
      return false;
    }
    if (!subscription) {
      message.textContent =
        'Notifications are blocked on this device. Allow them in your browser’s settings, then try again.';
      return false;
    }
    const { keys } = subscription.toJSON();
    endpoint = subscription.endpoint;
    add.elements.endpoint.value = endpoint;
    add.elements.p256dh.value = keys.p256dh;
    add.elements.auth.value = keys.auth;
    return true;
  }

  // unsubscribe takes this browser off the push service when the Remove form
  // is for this browser's own row. Another browser's row needs only the post.
  async function unsubscribe(remove) {
    const subscription = await push.current();
    if (subscription && subscription.endpoint === remove.dataset.endpoint) {
      // A failed unsubscribe is ignored. The row is deleted either way, so
      // nothing will be sent to the subscription again.
      await subscription.unsubscribe().catch(() => {});
      endpoint = null;
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
