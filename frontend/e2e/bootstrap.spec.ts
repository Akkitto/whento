/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { expect, test, type Page } from '@playwright/test';

declare global {
  interface Window {
    bootstrapAuthEvents?: unknown[];
  }
}

test.beforeEach(async ({ page }) => {
  await page.route('**/api/v1/auth/status', route =>
    route.fulfill({
      status: 200,
      json: { success: true, data: { needs_bootstrap: true, registration_enabled: false } },
    })
  );
});

async function fillBootstrap(page: Page) {
  await page.locator('#boot_key').fill('wrong-operator-boot-key-0123456789abcdef');
  await page.locator('#display_name').fill('Owner');
  await page.locator('#email').fill('owner@example.test');
  await page.locator('#password').fill('Str0ng!Passw0rd');
}

test('a wrong bootstrap key never refreshes or signs out another tab', async ({
  page,
  context,
}) => {
  let refreshes = 0;
  await page.route('**/api/v1/auth/refresh', route => {
    refreshes++;
    return route.fulfill({
      status: 401,
      json: { success: false, error: { code: 'UNAUTHORIZED' } },
    });
  });
  await page.route('**/api/v1/auth/bootstrap', route =>
    route.fulfill({
      status: 401,
      json: { success: false, error: { code: 'UNAUTHORIZED', message: 'Invalid bootstrap key' } },
    })
  );
  await page.goto('/bootstrap');
  const other = await context.newPage();
  await other.goto('/dev/preview.html');
  await other.evaluate(() => {
    const events: unknown[] = [];
    window.bootstrapAuthEvents = events;
    const channel = new BroadcastChannel('whento.auth');
    channel.onmessage = event => events.push(event.data);
  });
  await page.evaluate(() => localStorage.setItem('whento.session', '1'));
  await fillBootstrap(page);
  await page.getByRole('button', { name: 'Create administrator account' }).click();
  await expect(page.getByRole('alert')).toContainText('The boot key is invalid.');
  await expect(page).toHaveURL(/\/bootstrap$/);
  expect(refreshes).toBe(0);
  expect(await page.evaluate(() => localStorage.getItem('whento.session'))).toBe('1');
  // A delivered barrier proves the other tab's channel has processed messages
  // sent after the failed request; this is not an arbitrary sleep/no-event test.
  await page.evaluate(() => {
    const channel = new BroadcastChannel('whento.auth');
    channel.postMessage({ type: 'test-barrier' });
    channel.close();
  });
  await expect
    .poll(() => other.evaluate(() => window.bootstrapAuthEvents ?? []))
    .toContainEqual({ type: 'test-barrier' });
  expect(await other.evaluate(() => window.bootstrapAuthEvents ?? [])).not.toContainEqual({
    type: 'logout',
  });
});

test('a closed bootstrap stays visible until the operator chooses sign-in', async ({ page }) => {
  await page.route('**/api/v1/auth/bootstrap', route =>
    route.fulfill({ status: 409, json: { success: false, error: { code: 'CONFLICT' } } })
  );
  await page.goto('/bootstrap');
  await fillBootstrap(page);
  await page.getByRole('button', { name: 'Create administrator account' }).click();
  await expect(page.getByRole('alert')).toContainText('This instance is already configured.');
  await expect(page.getByRole('alert').getByRole('link', { name: /sign in/i })).toBeVisible();
  await expect(page.locator('form')).not.toBeVisible();
  await expect(page).toHaveURL(/\/bootstrap$/);
});
