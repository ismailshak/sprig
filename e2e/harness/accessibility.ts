import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';
import { AcceptScreen } from '../screens/accept';
import { AccountScreen } from '../screens/account';
import { ActivityScreen } from '../screens/activity';
import { AppearanceScreen } from '../screens/appearance';
import { ArchivedPlantsScreen } from '../screens/archived-plants';
import { CalendarScreen } from '../screens/calendar';
import { CloseAccountScreen } from '../screens/close-account';
import { DeleteGardenScreen } from '../screens/delete-garden';
import { ErrorPageScreen } from '../screens/error-page';
import { GardenScreen } from '../screens/garden';
import { InstallScreen } from '../screens/install';
import { InviteScreen } from '../screens/invite';
import { InvitedScreen } from '../screens/invited';
import { MoreScreen } from '../screens/more';
import { NoGardenScreen } from '../screens/no-garden';
import { NotificationsScreen } from '../screens/notifications';
import { PasskeysScreen } from '../screens/passkeys';
import { PeopleScreen } from '../screens/people';
import { PhotoFormScreen } from '../screens/photo-form';
import { PhotosScreen } from '../screens/photos';
import { PlantScreen } from '../screens/plant';
import { PlantFormScreen } from '../screens/plant-form';
import { PlantsScreen } from '../screens/plants';
import { RecoverScreen } from '../screens/recover';
import { RecoveryScreen } from '../screens/recovery';
import { RemindersScreen } from '../screens/reminders';
import { SetupScreen } from '../screens/setup';
import { SignInScreen } from '../screens/signin';
import { TodayScreen } from '../screens/today';
import { TokensScreen } from '../screens/tokens';
import { archivedPlants, invites, people, plants } from './garden';
import { photos as files } from './photos';

// CheckedPage is one page in the accessibility run. name is the page's name in
// the app. as is the seeded handle to sign in as, or undefined for a page
// served before sign-in. open navigates to the page through its screen object.
// Where the last step has no screen object, open waits for the page's heading,
// because axe passes when it scans the wrong page.
export type CheckedPage = { name: string; as?: string; open: (page: Page) => Promise<void> };

// withOnePhoto adds a photo to Big Fella, because the seed writes none. It
// needs JavaScript, because the photo field resizes the file in the browser.
async function withOnePhoto(page: Page): Promise<void> {
  const form = new PhotoFormScreen(page);
  await form.open(plants.bigFella);
  await form.choose(files.gpsTagged);
  await form.preview().waitFor();
  await form.submit();
}

// pages is every page in the app, plus the Log care and Switch garden sheets
// over Today and a day's sheet over the calendar. A new page in the app gets a
// line here.
export const pages: CheckedPage[] = [
  { name: 'Sign in', open: (page) => new SignInScreen(page).open() },
  { name: 'You’re invited', open: (page) => new InvitedScreen(page).open(invites.sitter.token) },
  { name: 'Recover an account', open: (page) => new RecoverScreen(page).open() },
  { name: 'Set up your garden', open: (page) => new SetupScreen(page).open() },
  // The same route opened by an account that already exists. It redirects to
  // /setup/signed-in, a page that asks for the garden's name alone.
  { name: 'Set up a garden as this account', as: people.robin.handle, open: (page) => new SetupScreen(page).open() },
  // Robin is in Upstairs and not in Home, so Robin can accept the seeded
  // invite to Home.
  {
    name: 'Join a garden',
    as: people.robin.handle,
    open: (page) => new AcceptScreen(page).open(invites.sitter.token),
  },
  // Clare's only membership has ended.
  {
    name: 'No garden',
    as: people.clare.handle,
    open: async (page) => {
      await new TodayScreen(page).open();
      await new NoGardenScreen(page).heading().waitFor();
    },
  },
  { name: 'Reminders', as: people.ellie.handle, open: (page) => new RemindersScreen(page).open() },
  { name: 'Install sprig', as: people.ellie.handle, open: (page) => new InstallScreen(page).open() },
  { name: 'Today', as: people.ellie.handle, open: (page) => new TodayScreen(page).open() },
  {
    name: 'Log care sheet',
    as: people.ellie.handle,
    open: async (page) => {
      const today = new TodayScreen(page);
      await today.open();
      await today.openSheet(plants.nigel, 'water');
      await page.getByRole('dialog').waitFor();
    },
  },
  // Sam is in both gardens, so Today has the switch.
  {
    name: 'Switch garden sheet',
    as: people.sam.handle,
    open: async (page) => {
      const today = new TodayScreen(page);
      await today.open();
      await today.switchGarden().click();
      await today.gardenSheet().waitFor();
    },
  },
  { name: 'Plants', as: people.ellie.handle, open: (page) => new PlantsScreen(page).open() },
  { name: 'Plant', as: people.ellie.handle, open: (page) => new PlantScreen(page).open(plants.bigFella) },
  { name: 'Add plant', as: people.ellie.handle, open: (page) => new PlantFormScreen(page).openNew() },
  { name: 'Edit plant', as: people.ellie.handle, open: (page) => new PlantFormScreen(page).openEdit(plants.bigFella) },
  {
    name: 'Photos',
    as: people.ellie.handle,
    open: async (page) => {
      await withOnePhoto(page);
      await new PhotosScreen(page).open(plants.bigFella);
    },
  },
  {
    name: 'Photo',
    as: people.ellie.handle,
    open: async (page) => {
      await withOnePhoto(page);
      await new PhotosScreen(page).openTile(0);
    },
  },
  { name: 'Add photo', as: people.ellie.handle, open: (page) => new PhotoFormScreen(page).open(plants.bigFella) },
  { name: 'Archived plants', as: people.ellie.handle, open: (page) => new ArchivedPlantsScreen(page).open() },
  // Archived plants is the only page that links to an archived plant.
  {
    name: 'Archived plant',
    as: people.ellie.handle,
    open: async (page) => {
      const archived = new ArchivedPlantsScreen(page);
      await archived.open();
      await archived.row(archivedPlants.barry.name).click();
      await new PlantScreen(page).heading().waitFor();
    },
  },
  { name: 'Activity', as: people.ellie.handle, open: (page) => new ActivityScreen(page).open() },
  { name: 'Calendar', as: people.ellie.handle, open: (page) => new CalendarScreen(page).open() },
  {
    name: 'Day sheet',
    as: people.ellie.handle,
    open: async (page) => {
      const calendar = new CalendarScreen(page);
      await calendar.open();
      await calendar.openDay(calendar.firstDayWithCareDue());
      await calendar.daySheet().waitFor();
    },
  },
  { name: 'More', as: people.ellie.handle, open: (page) => new MoreScreen(page).open() },
  { name: 'Account', as: people.ellie.handle, open: (page) => new AccountScreen(page).open() },
  { name: 'Recovery codes', as: people.ellie.handle, open: (page) => new RecoveryScreen(page).open() },
  // Sam owns no garden, so Close account shows the button rather than the
  // reason the account cannot be closed.
  { name: 'Close account', as: people.sam.handle, open: (page) => new CloseAccountScreen(page).open() },
  { name: 'Passkeys', as: people.ellie.handle, open: (page) => new PasskeysScreen(page).open() },
  { name: 'Appearance', as: people.ellie.handle, open: (page) => new AppearanceScreen(page).open() },
  { name: 'Notifications', as: people.ellie.handle, open: (page) => new NotificationsScreen(page).open() },
  { name: 'Garden', as: people.ellie.handle, open: (page) => new GardenScreen(page).open() },
  { name: 'Delete garden', as: people.ellie.handle, open: (page) => new DeleteGardenScreen(page).open() },
  { name: 'People', as: people.ellie.handle, open: (page) => new PeopleScreen(page).open() },
  { name: 'Invite someone', as: people.ellie.handle, open: (page) => new InviteScreen(page).open() },
  { name: 'Tokens', as: people.ellie.handle, open: (page) => new TokensScreen(page).open() },
  {
    name: 'Page not found',
    as: people.ellie.handle,
    open: async (page) => {
      await page.goto('/no-such-page');
      await new ErrorPageScreen(page).heading('Page not found').waitFor();
    },
  },
];

// Axe rules the scan skips. Each entry gets a comment saying why.
const disabledRules: string[] = [];

// violations runs axe once over the page and returns one line per failing
// element: the rule, what it checks, and the element's selector.
export async function violations(page: Page): Promise<string[]> {
  const results = await new AxeBuilder({ page }).disableRules(disabledRules).analyze();
  return results.violations.flatMap((violation) =>
    violation.nodes.map((node) => `${violation.id} (${violation.help}): ${node.target.join(' ')}`),
  );
}
