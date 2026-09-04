import { test as base } from '@playwright/test';
import { seed } from './database';

// Every test starts from the seed's garden, whatever the test before it wrote.
export const test = base.extend<{ garden: void }>({
  garden: [
    async ({}, use) => {
      await seed();
      await use();
    },
    { auto: true },
  ],
});

export { expect } from '@playwright/test';
