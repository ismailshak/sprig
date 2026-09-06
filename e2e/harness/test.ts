import { test as base } from '@playwright/test';
import { AccountScreen } from '../screens/account';
import { ActivityScreen } from '../screens/activity';
import { InstallScreen } from '../screens/install';
import { MoreScreen } from '../screens/more';
import { NotificationsScreen } from '../screens/notifications';
import { PasskeysScreen } from '../screens/passkeys';
import { PlantScreen } from '../screens/plant';
import { PlantFormScreen } from '../screens/plant-form';
import { PlantsScreen } from '../screens/plants';
import { SheetScreen } from '../screens/sheet';
import { TodayScreen } from '../screens/today';
import { seed, stacks, type Stack } from './database';

type Screens = {
  account: AccountScreen;
  activity: ActivityScreen;
  install: InstallScreen;
  more: MoreScreen;
  notifications: NotificationsScreen;
  passkeys: PasskeysScreen;
  plant: PlantScreen;
  plantForm: PlantFormScreen;
  plants: PlantsScreen;
  sheet: SheetScreen;
  today: TodayScreen;
};

export const test = base.extend<{ garden: void } & Screens, { stack: Stack }>({
  // A worker keeps one stack for its whole life, picked by its index. Two
  // workers never share a database.
  stack: [
    async ({}, use, workerInfo) => {
      const stack = stacks(process.env)[workerInfo.parallelIndex];
      if (!stack) {
        throw new Error(`worker ${workerInfo.parallelIndex} has no stack: the suite runs one worker per stack`);
      }
      await use(stack);
    },
    { scope: 'worker' },
  ],
  baseURL: async ({ stack }, use) => {
    await use(stack.baseURL);
  },
  // Every test starts from a fresh seed, whatever the previous test wrote.
  garden: [
    async ({ stack }, use) => {
      await seed(stack.databaseURL);
      await use();
    },
    { auto: true },
  ],
  account: async ({ page }, use) => {
    await use(new AccountScreen(page));
  },
  activity: async ({ page }, use) => {
    await use(new ActivityScreen(page));
  },
  install: async ({ page }, use) => {
    await use(new InstallScreen(page));
  },
  more: async ({ page }, use) => {
    await use(new MoreScreen(page));
  },
  notifications: async ({ page }, use) => {
    await use(new NotificationsScreen(page));
  },
  passkeys: async ({ page }, use) => {
    await use(new PasskeysScreen(page));
  },
  plant: async ({ page }, use) => {
    await use(new PlantScreen(page));
  },
  plantForm: async ({ page }, use) => {
    await use(new PlantFormScreen(page));
  },
  plants: async ({ page }, use) => {
    await use(new PlantsScreen(page));
  },
  sheet: async ({ page }, use) => {
    await use(new SheetScreen(page));
  },
  today: async ({ page }, use) => {
    await use(new TodayScreen(page));
  },
});

export { expect } from '@playwright/test';
