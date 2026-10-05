/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { expect, test } from '@playwright/test';

const token = 'abcdef0123456789'.repeat(4);
const cleanPath = '/auth/magic-link/verify/';

test.beforeEach(async ({ page }) => {
  await page.route('**/api/v1/**', route =>
    route.fulfill({ status: 200, json: { success: true, data: { available: false } } })
  );
});

test('magic-link confirmation scrubs browser and router history, including Back', async ({
  page,
}) => {
  let verifications = 0;
  await page.route('**/api/v1/auth/magic-link/verify', route => {
    verifications++;
    return route.fulfill({ status: 500, json: { success: false } });
  });
  await page.goto(`/auth/magic-link/verify/${token}`);
  await expect(page).toHaveURL(new RegExp(`${cleanPath}$`));
  await expect(page.getByRole('button', { name: 'Continue signing in' })).toBeVisible();
  expect(await page.evaluate(() => history.state.current)).toBe(cleanPath);
  expect(await page.evaluate(() => JSON.stringify(history.state))).not.toContain(token);
  // The secret lives only in this component until explicitly confirmed.
  expect(verifications).toBe(0);
  await page.locator('a[href="/"]').first().click();
  await expect(page).toHaveURL('http://127.0.0.1:8099/');
  expect(await page.evaluate(() => history.state.back)).toBe(cleanPath);
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`${cleanPath}$`));
  await expect(page.getByText('Please open the sign-in link from your email again.')).toBeVisible();
  expect(await page.evaluate(() => JSON.stringify(history.state))).not.toContain(token);
  expect(verifications).toBe(0);
});

test('explicit confirmation uses the in-memory proof and keeps MFA navigation history clean', async ({
  page,
}) => {
  let postedToken = '';
  await page.route('**/api/v1/auth/magic-link/verify', route => {
    postedToken = route.request().postDataJSON().token;
    expect(route.request().method()).toBe('POST');
    expect(route.request().headers()['x-whento-auth-intent']).toBe('magic-link');
    return route.fulfill({
      status: 200,
      json: { success: true, data: { require_mfa: true, temp_token: 'pending-mfa' } },
    });
  });
  await page.goto(`/auth/magic-link/verify/${token}`);
  await expect(page).toHaveURL(new RegExp(`${cleanPath}$`));
  expect(postedToken).toBe('');
  await page.getByRole('button', { name: 'Continue signing in' }).click();
  await expect(page).toHaveURL(/\/verify-mfa$/);
  expect(postedToken).toBe(token);
  expect(await page.evaluate(() => history.state.back)).toBe(cleanPath);
  expect(await page.evaluate(() => JSON.stringify(history.state))).not.toContain(token);
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain(token);
});

test('reloading the scrubbed magic-link route shows recovery instructions, not 404', async ({
  page,
}) => {
  await page.goto(`/auth/magic-link/verify/${token}`);
  await expect(page).toHaveURL(new RegExp(`${cleanPath}$`));
  await page.reload();
  await expect(page.getByText('Please open the sign-in link from your email again.')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Invalid Magic Link' })).toBeVisible();
});

test('MFA-required reset without a temporary token never claims success or redirects', async ({
  page,
}) => {
  await page.clock.install();
  await page.route('**/api/v1/auth/reset-password', route =>
    route.fulfill({ status: 200, json: { success: true, data: { require_mfa: true } } })
  );
  await page.goto(`/reset-password/${token}`);
  await page.locator('#new-password').fill('Valid1!Password');
  await page.locator('#confirm-password').fill('Valid1!Password');
  await page.getByRole('button', { name: 'Reset Password', exact: true }).click();
  await expect(page.locator('form .text-danger-800')).toBeVisible();
  await page.clock.fastForward(3000);
  await expect(page).toHaveURL(new RegExp(`/reset-password/${token}$`));
  await expect(page.getByText('Password Reset Successful!', { exact: true })).not.toBeVisible();
  await expect(page.locator('form')).toBeVisible();
});
