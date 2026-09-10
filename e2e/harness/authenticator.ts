import type { Page } from '@playwright/test';

// A passkey is bound to the relying party id and needs a real device to sign,
// so the suite uses Chrome DevTools' virtual authenticator instead. Only
// Chromium has it. That is why the passkey tests run in a project of their own.

// Device is what a virtual authenticator can do. The two false cases are the
// ones registration refuses: a device that cannot store the passkey, and one
// that does not check who is using it.
export type Device = {
  // stores is whether the authenticator can hold a discoverable credential.
  stores: boolean;
  // verifies is whether it can check who is using it, with a screen lock, a
  // fingerprint or a PIN.
  verifies: boolean;
  // unlocked is whether that check passes. A device that can verify and is
  // locked completes the prompt without verifying.
  unlocked: boolean;
};

// aWorkingDevice is a phone or laptop with a screen lock. Almost every passkey
// is registered from one.
export const aWorkingDevice: Device = { stores: true, verifies: true, unlocked: true };

// Passkey is a credential the seed wrote. credentialId and userHandle are the
// seed's UUIDs for the passkey row and the account. privateKey is the private
// half of the key pair, in PKCS#8 and base64 encoded.
export type Passkey = { credentialId: string; userHandle: string; privateKey: string };

// attach adds a virtual authenticator to the page's browser and returns a
// function that removes it. Everything the page does through
// navigator.credentials goes to this authenticator until then. A passkey is
// added as a discoverable credential, so a sign-in finds it without
// registering one first. The page has to be on the app already, because the
// credential is stored under the hostname in the page's URL.
export async function attach(page: Page, device: Device, passkey?: Passkey): Promise<() => Promise<void>> {
  const client = await page.context().newCDPSession(page);
  await client.send('WebAuthn.enable');
  const { authenticatorId } = await client.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2',
      transport: 'internal',
      hasResidentKey: device.stores,
      hasUserVerification: device.verifies,
      isUserVerified: device.unlocked,
      // automaticPresenceSimulation satisfies the presence check, because no
      // test can touch the device.
      automaticPresenceSimulation: true,
    },
  });

  if (passkey) {
    const rpId = new URL(page.url()).hostname;
    if (!rpId) {
      throw new Error('open a page on the app before loading a passkey: the passkey is bound to the hostname');
    }
    await client.send('WebAuthn.addCredential', {
      authenticatorId,
      credential: {
        credentialId: base64Of(passkey.credentialId),
        isResidentCredential: true,
        rpId,
        privateKey: passkey.privateKey,
        userHandle: base64Of(passkey.userHandle),
        signCount: 0,
        // Both set, as they are on a synced passkey. The seed stores the
        // same two flags, because the server refuses a sign-in whose
        // backup-eligible flag differs from the stored one.
        backupEligibility: true,
        backupState: true,
      },
    });
  }

  return async () => {
    await client.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId });
    await client.detach();
  };
}

// base64Of returns a UUID's 16 bytes, base64 encoded. The DevTools protocol
// takes binary fields in that form.
function base64Of(uuid: string): string {
  return Buffer.from(uuid.replaceAll('-', ''), 'hex').toString('base64');
}

// withDevice runs press with device plugged into the browser. The
// authenticator is removed afterwards so nothing it registered is offered to
// the next test.
export async function withDevice(page: Page, device: Device, press: () => Promise<void>): Promise<void> {
  const detach = await attach(page, device);
  try {
    await press();
  } finally {
    await detach();
  }
}
