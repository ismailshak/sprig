import { test as base } from '@playwright/test';
import { PlantsScreen } from '../screens/plants';
import { SheetScreen } from '../screens/sheet';
import { TodayScreen } from '../screens/today';
import { seed } from './database';

type Screens = {
  today: TodayScreen;
  plants: PlantsScreen;
  sheet: SheetScreen;
};

// Every test starts from the seed's garden, whatever the test before it wrote.
export const test = base.extend<{ garden: void } & Screens>({
  garden: [
    async ({}, use) => {
      await seed();
      await use();
    },
    { auto: true },
  ],
  today: async ({ page }, use) => {
    await use(new TodayScreen(page));
  },
  plants: async ({ page }, use) => {
    await use(new PlantsScreen(page));
  },
  sheet: async ({ page }, use) => {
    await use(new SheetScreen(page));
  },
});

export { expect } from '@playwright/test';
