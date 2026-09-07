import { expect, test as base } from '@playwright/test';
import { AcceptScreen } from '../screens/accept';
import { AccountScreen } from '../screens/account';
import { ActivityScreen } from '../screens/activity';
import { GardenScreen } from '../screens/garden';
import { InstallScreen } from '../screens/install';
import { InviteScreen } from '../screens/invite';
import { InvitedScreen } from '../screens/invited';
import { MoreScreen } from '../screens/more';
import { NotificationsScreen } from '../screens/notifications';
import { PasskeysScreen } from '../screens/passkeys';
import { PeopleScreen } from '../screens/people';
import { PlantScreen } from '../screens/plant';
import { PlantFormScreen } from '../screens/plant-form';
import { PlantsScreen } from '../screens/plants';
import { RecoveryScreen } from '../screens/recovery';
import { SetupScreen } from '../screens/setup';
import { SheetScreen } from '../screens/sheet';
import { SignInScreen } from '../screens/signin';
import { TodayScreen } from '../screens/today';
import { TokensScreen } from '../screens/tokens';
import { seed, stacks, type Stack } from './database';

type Screens = {
  accept: AcceptScreen;
  account: AccountScreen;
  activity: ActivityScreen;
  garden: GardenScreen;
  install: InstallScreen;
  invite: InviteScreen;
  invited: InvitedScreen;
  more: MoreScreen;
  notifications: NotificationsScreen;
  passkeys: PasskeysScreen;
  people: PeopleScreen;
  plant: PlantScreen;
  plantForm: PlantFormScreen;
  plants: PlantsScreen;
  recovery: RecoveryScreen;
  setup: SetupScreen;
  sheet: SheetScreen;
  signin: SignInScreen;
  today: TodayScreen;
  tokens: TokensScreen;
};

export const test = base.extend<{ seededGarden: void; cspViolations: void } & Screens, { stack: Stack }>({
  // A worker keeps one stack for its whole life, picked by its index.
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
  seededGarden: [
    async ({ stack }, use) => {
      await seed(stack.databaseURL);
      await use();
    },
    { auto: true },
  ],
  // The browser reports a blocked script or style as a console error and shows
  // nothing on the page, so without this fixture a test passes while a swap or
  // the undo bar is broken. Chromium and WebKit both put "Content Security
  // Policy" in the message.
  cspViolations: [
    async ({ page }, use) => {
      const violations: string[] = [];
      page.on('console', (message) => {
        if (message.type() === 'error' && message.text().includes('Content Security Policy')) {
          violations.push(message.text());
        }
      });
      await use();
      expect(violations).toEqual([]);
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
  accept: async ({ page }, use) => {
    await use(new AcceptScreen(page));
  },
  invite: async ({ page }, use) => {
    await use(new InviteScreen(page));
  },
  invited: async ({ page }, use) => {
    await use(new InvitedScreen(page));
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
  people: async ({ page }, use) => {
    await use(new PeopleScreen(page));
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
  recovery: async ({ page }, use) => {
    await use(new RecoveryScreen(page));
  },
  setup: async ({ page }, use) => {
    await use(new SetupScreen(page));
  },
  sheet: async ({ page }, use) => {
    await use(new SheetScreen(page));
  },
  signin: async ({ page }, use) => {
    await use(new SignInScreen(page));
  },
  today: async ({ page }, use) => {
    await use(new TodayScreen(page));
  },
  tokens: async ({ page }, use) => {
    await use(new TokensScreen(page));
  },
});

export { expect } from '@playwright/test';
