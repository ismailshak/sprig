/* Registering a passkey and signing in with one. Both do the same three
   things: post for a challenge, hand it to the browser's credential API, and
   post the credential it returns. Only the browser can talk to the
   authenticator. The forms it runs on are the only ones in sprig that need a
   script.

   A form here has data-passkey="create" or data-passkey="get", a
   data-challenge URL to post for the challenge, and a data-field naming the
   hidden input the credential goes in. Its submit button starts disabled, and
   the note named by data-note says the form needs a script. This script
   enables the button and removes the note.

   The form's fields are posted with the request for the challenge. Set up
   your garden needs the display name before the passkey is made, because the
   browser stores that name with the passkey.

   A form with data-swap, Add passkey on Passkeys, posts the credential with
   htmx and replaces the element with that id. The other forms redirect to
   another page when they succeed, so they post it as an ordinary form. */
(function () {
  const forms = document.querySelectorAll('form[data-passkey]');
  // The three JSON helpers arrived in browsers later than the credential API
  // itself, so a browser can have one without the others. Without them the
  // button stays disabled and the note stays, rather than a press that fails
  // after the prompt.
  const supported =
    window.PublicKeyCredential &&
    typeof PublicKeyCredential.parseCreationOptionsFromJSON === 'function' &&
    typeof PublicKeyCredential.parseRequestOptionsFromJSON === 'function' &&
    typeof PublicKeyCredential.prototype.toJSON === 'function';
  if (forms.length === 0 || !supported) return;

  for (const form of forms) {
    const field = form.elements.namedItem(form.dataset.field);
    const button = form.querySelector('button[type="submit"]');
    // The message line is looked up each time, because the swap on Passkeys
    // replaces it.
    const message = () => document.getElementById(form.dataset.message);
    const note = document.getElementById(form.dataset.note);
    if (!field || !button) continue;

    button.disabled = false;
    if (note) note.remove();

    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      button.disabled = true;
      const line = message();
      if (line) line.textContent = '';

      try {
        const response = await fetch(form.dataset.challenge, {
          method: 'POST',
          body: new URLSearchParams(new FormData(form)),
        });
        // A 422 says the server refused a field. The form is submitted as
        // it is, so the server renders the page again with the message under
        // that field. No passkey is made for a form the post would refuse.
        if (response.status === 422) {
          form.submit();
          return;
        }
        // The body of a 429 is a sentence for the person to read, so it is
        // shown as it is. Any other failed response gets the generic sentence
        // in refusal below.
        if (response.status === 429) throw new Refused(await response.text());
        if (!response.ok) throw new Error('the challenge was refused');
        const options = await response.json();

        // parseCreationOptionsFromJSON and parseRequestOptionsFromJSON decode
        // the base64url the server sent into the buffers the credential API
        // takes. toJSON encodes the credential the same way.
        const credential =
          form.dataset.passkey === 'create'
            ? await navigator.credentials.create({
                publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(options.publicKey),
              })
            : await navigator.credentials.get({
                publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(options.publicKey),
              });

        field.value = JSON.stringify(credential.toJSON());
        if (form.dataset.swap) {
          // htmx rejects with no error when the post cannot reach the server.
          // The page's swap script has already said so in the live region.
          await htmx
            .ajax('POST', form.action, {
              source: form,
              target: '#' + form.dataset.swap,
              swap: 'outerHTML settle:0ms',
            })
            .catch(() => {});
          button.disabled = false;
          return;
        }
        // submit() does not fire this event again, so the post goes straight
        // out with the credential in the field.
        form.submit();
      } catch (error) {
        button.disabled = false;
        const line = message();
        if (line) line.textContent = refusal(error, form.dataset.passkey);
      }
    });
  }

  /* Refused is an Error holding a message the server sent for the person to
     read. refusal returns that message unchanged. */
  class Refused extends Error {
    constructor(sentence) {
      super(sentence);
      this.name = 'Refused';
    }
  }

  /* refusal turns what the credential API threw into a sentence. ceremony is
     the form's data-passkey, create or get.

     NotAllowedError covers a cancelled prompt, a prompt nobody answered, and a
     device that cannot do what was asked: for a registration, store a passkey
     or check who is using it, and for a sign-in, a device holding no passkey
     for this site. Chrome reports all of those under the one name on purpose,
     so that a site cannot find out what somebody's devices can do by asking.
     The sentence names every case rather than guessing at one. */
  function refusal(error, ceremony) {
    switch (error.name) {
      case 'Refused':
        return error.message.trim();
      case 'NotAllowedError':
        if (ceremony === 'get') {
          return 'Sign-in was cancelled, or this device has no passkey for sprig.';
        }
        return 'No passkey was added. You may have cancelled, or this device can’t store passkeys.';
      case 'InvalidStateError':
        return 'This device already has a passkey for sprig.';
      case 'NotSupportedError':
      case 'ConstraintError':
        return 'This device can’t store a passkey. It needs a screen lock, fingerprint or PIN.';
      case 'SecurityError':
        return 'Passkeys need a secure (https) connection to the address sprig is configured for.';
      default:
        return 'Something went wrong. Try again.';
    }
  }
})();
