import { test as base } from '@playwright/test';
import { AccountScreen } from '../screens/account';
import { ActivityScreen } from '../screens/activity';
import { GardenScreen } from '../screens/garden';
import { InstallScreen } from '../screens/install';
import { MoreScreen } from '../screens/more';
import { NotificationsScreen } from '../screens/notifications';
import { PasskeysScreen } from '../screens/passkeys';
import { PlantScreen } from '../screens/plant';
import { PlantFormScreen } from '../screens/plant-form';
import { PlantsScreen } from '../screens/plants';
import { SheetScreen } from '../screens/sheet';
import { TodayScreen } from '../screens/today';
import { seed } from './database';

type Screens = {
  account: AccountScreen;
  activity: ActivityScreen;
  garden: GardenScreen;
  install: InstallScreen;
  more: MoreScreen;
  notifications: NotificationsScreen;
  passkeys: PasskeysScreen;
  today: TodayScreen;
  plant: PlantScreen;
  plantForm: PlantFormScreen;
  plants: PlantsScreen;
  sheet: SheetScreen;
};

// Every test starts from a fresh seed, whatever the previous test wrote.
export const test = base.extend<{ seededGarden: void } & Screens>({
  seededGarden: [
    async ({}, use) => {
      await seed();
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
  garden: async ({ page }, use) => {
    await use(new GardenScreen(page));
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
  today: async ({ page }, use) => {
    await use(new TodayScreen(page));
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
});

export { expect } from '@playwright/test';
