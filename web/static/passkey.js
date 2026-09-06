/* Registering a passkey and signing in with one. Both do the same three
   things: post for a challenge, hand it to the browser's credential API, and
   submit the browser's answer as an ordinary form post. Only the browser can
   talk to the authenticator. These two forms are the only ones in sprig that
   need a script.

   A form here has data-passkey="create" or data-passkey="get", a
   data-challenge URL to post for the challenge, and a data-field naming the
   hidden input the answer goes in. Its submit button starts disabled, and the
   note named by data-note says the form needs a script. This script enables the
   button and removes the note. */
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
    const message = document.getElementById(form.dataset.message);
    const note = document.getElementById(form.dataset.note);
    if (!field || !button) continue;

    button.disabled = false;
    if (note) note.remove();

    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      button.disabled = true;
      if (message) message.textContent = '';

      try {
        const response = await fetch(form.dataset.challenge, { method: 'POST' });
        // The body of a 429 is a sentence for the person to read, so it is
        // shown as it is. Any other failed response gets the generic sentence
        // in refusal below.
        if (response.status === 429) throw new Refused(await response.text());
        if (!response.ok) throw new Error('the challenge was refused');
        const options = await response.json();

        // parseCreationOptionsFromJSON and parseRequestOptionsFromJSON decode
        // the base64url the server sent into the buffers the credential API
        // takes. toJSON encodes the answer the same way.
        const credential =
          form.dataset.passkey === 'create'
            ? await navigator.credentials.create({
                publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(options.publicKey),
              })
            : await navigator.credentials.get({
                publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(options.publicKey),
              });

        field.value = JSON.stringify(credential.toJSON());
        // submit() does not fire this event again, so the post goes straight
        // out with the answer in the field.
        form.submit();
      } catch (error) {
        button.disabled = false;
        if (message) message.textContent = refusal(error, form.dataset.passkey);
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
          return 'Nothing signed in. Either you cancelled, or this device holds no passkey for sprig. The browser does not say which.';
        }
        return 'No passkey was added. Either you cancelled, or this device cannot do what sprig needs: store the passkey itself, and check that it is you with a screen lock, a fingerprint or a PIN. The browser does not say which.';
      case 'InvalidStateError':
        return 'This device already has a passkey for sprig.';
      case 'NotSupportedError':
      case 'ConstraintError':
        return 'This device cannot store a passkey that sprig can use. It has to be able to save the passkey and to check that it is you, with a screen lock, a fingerprint or a PIN.';
      case 'SecurityError':
        return 'Passkeys need a secure connection and a domain the browser accepts for this page. Either this page is not on https, or sprig is set up under a domain that does not match it.';
      default:
        return 'Something went wrong talking to this device. Try again.';
    }
  }
})();
