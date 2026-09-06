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

// attach adds a virtual authenticator to the page's browser and returns a
// function that removes it. Everything the page does through
// navigator.credentials goes to this authenticator until then.
export async function attach(page: Page, device: Device): Promise<() => Promise<void>> {
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

  return async () => {
    await client.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId });
    await client.detach();
  };
}
