const { test, expect } = require('@playwright/test');

test('reauth and view escalation content', async ({ page, baseURL }) => {
  // This test requires the backend to be running and ADMIN_TEST_PASSWORD env var set.
  if (!process.env.ADMIN_TEST_PASSWORD) {
    test.skip();
  }

  await page.goto(`${baseURL}/admin/escalations`);
  const firstView = page.locator('.view-content').first();
  await expect(firstView).toBeVisible();
  await firstView.click();
  // Wait for reauth modal
  const modal = page.locator('#adminui-reauth-modal');
  await expect(modal).toBeVisible();
  await page.fill('#adminui-reauth-password', process.env.ADMIN_TEST_PASSWORD);
  await page.click('#adminui-reauth-submit');
  await page.waitForSelector('#adminui-viewer-content', { timeout: 5000 });
  const content = await page.textContent('#adminui-viewer-content');
  expect(content).toBeTruthy();
});
