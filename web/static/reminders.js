/* The offer to turn on reminders in this browser: the Reminders page at the
   end of setup, and the banner on Today. Each is the element with id
   reminders, holding Turn on notifications and Not now. The press asks for
   permission, subscribes this browser and posts the subscription. Only the
   push API can do that.

   The element has data-key, the VAPID public key to subscribe with, and
   data-subscribe, the URL the subscription is posted to. The Reminders page
   adds data-next, the page opened once the browser is subscribed. The banner
   has no data-next and hides itself instead.

   The Reminders page is shown from the start. In a browser with no push API
   it is hidden and the element with id reminders-install is shown in its
   place: the iPhone's install steps, because on an iPhone that is a browser
   where sprig is not on the Home Screen.

   The banner starts hidden. It is shown only when the app is opened
   installed, this browser has no push subscription, permission has not been
   refused, and Not now has not been pressed before. Not now is remembered in
   localStorage, so it is per browser like the subscription. */
(function () {
  const offer = document.getElementById('reminders');
  if (!offer || !window.sprigPush) return;
  const push = window.sprigPush;
  const on = document.getElementById('reminders-on');
  const skip = document.getElementById('reminders-skip');
  const message = document.getElementById('reminders-error');
  const next = offer.dataset.next;

  const dismissedKey = 'sprig.reminders-dismissed';

  if (next) {
    if (!push.supported) {
      offer.hidden = true;
      document.getElementById('reminders-install').hidden = false;
      return;
    }
  } else {
    bannerWanted().then((wanted) => {
      offer.hidden = !wanted;
    });
    skip.addEventListener('click', () => {
      localStorage.setItem(dismissedKey, '1');
      offer.hidden = true;
    });
  }

  let pressing = false;
  on.addEventListener('click', async (event) => {
    event.preventDefault();
    if (pressing) return;
    pressing = true;
    message.textContent = '';
    try {
      const permission = await push.subscribe(offer.dataset.key, offer.dataset.subscribe);
      if (permission !== 'granted') {
        message.textContent =
          'Notifications are blocked on this device. Allow them in your browser’s settings, then try again.';
        return;
      }
    } catch {
      message.textContent = 'Notifications couldn’t be turned on. Try again in a minute.';
      return;
    } finally {
      pressing = false;
    }
    if (next) {
      location.assign(next);
    } else {
      offer.hidden = true;
    }
  });

  // bannerWanted reports whether the banner is shown. An installed app is
  // display-mode: standalone, or navigator.standalone on iOS, where the media
  // query is not matched.
  async function bannerWanted() {
    const installed = matchMedia('(display-mode: standalone)').matches || navigator.standalone === true;
    if (!installed || !push.supported || Notification.permission !== 'default') return false;
    if (localStorage.getItem(dismissedKey)) return false;
    return (await push.current()) === null;
  }
})();
