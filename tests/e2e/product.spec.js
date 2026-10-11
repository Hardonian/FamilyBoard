const { test, expect } = require('@playwright/test');
const AxeBuilder = require('@axe-core/playwright').default;
const path = require('path');

test('kiosk core navigation, search, and accessible interaction', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('#nav-title')).toBeVisible();
  await expect(page.locator('#btn-add-event:visible, #btn-mobile-fab-add:visible')).toBeVisible();

  const search = page.getByRole('searchbox', { name: 'Search visible events' });
  await search.fill('soccer');
  await expect(search).toHaveValue('soccer');

  await page.locator('[data-view="week"]:visible').click();
  await expect(page.locator('#week-view-container')).toBeVisible();

  const results = await new AxeBuilder({ page })
    .disableRules(['color-contrast'])
    .analyze();
  expect(results.violations).toEqual([]);
});

test('login visual shell remains stable', async ({ page }) => {
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: /admin/i })).toBeVisible();
  await expect(page).toHaveScreenshot('admin-login.png', {
    animations: 'disabled',
    stylePath: path.join(__dirname, 'screenshot.css'),
    maxDiffPixelRatio: 0.05
  });
});

test('mobile kiosk exposes thumb navigation and explicit connectivity', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'mobile-chromium', 'mobile-only check');
  await page.goto('/');
  await expect(page.getByRole('navigation', { name: 'Mobile Navigation' })).toBeVisible();
  await expect(page.locator('#status-badge')).toBeVisible();
});
