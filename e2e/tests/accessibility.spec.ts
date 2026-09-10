import { pages, violations } from '../harness/accessibility';
import { signIn } from '../harness/signin';
import { expect, test } from '../harness/test';

// Each test scans one page with axe. Axe catches a control with no name, a
// field with no label, a skipped heading level and contrast under the ratio.
// It does not check where focus goes, the tab order, or whether a swap is
// announced.

for (const checked of pages) {
  test(`${checked.name} has no axe violations`, async ({ page }) => {
    if (checked.as) {
      await signIn(page, checked.as);
    }
    await checked.open(page);

    expect(await violations(page)).toEqual([]);
  });
}
