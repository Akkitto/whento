/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { expect, test } from '@playwright/test';

const PREVIEW = '/dev/preview-dashboard.html';
const KEY = 'dashboard:preview-user:prefs';

test('two real tabs retain pins, sorting and view changes, including stale-tab sign-out', async ({
  page,
  context,
}) => {
  const other = await context.newPage();
  await page.goto(PREVIEW);
  await other.goto(PREVIEW);
  const pin = (tab: typeof page) =>
    tab.locator('article[data-calendar-id="alpha"] [data-action="pin"]');
  await pin(page).click();
  await expect(pin(other)).toHaveAttribute('aria-pressed', 'true');
  await other.selectOption('#dashboard-sort', 'name-desc');
  await expect(page.locator('#dashboard-sort')).toHaveValue('name-desc');
  await page.click('[data-action="view-expanded"]');
  await expect(other.locator('[data-action="view-expanded"]')).toHaveAttribute(
    'aria-pressed',
    'true'
  );
  const saved = await page.evaluate(key => localStorage.getItem(key), KEY);
  expect(JSON.parse(saved!)).toMatchObject({
    pinnedIds: ['alpha'],
    sortDirection: 'desc',
    viewMode: 'expanded',
  });

  // Use the real dev application's auth store; changing the identity exercises
  // the sign-out watcher without posting to an actual authentication endpoint.
  await other.evaluate(async () => {
    const moduleUrl = '/src/stores/auth.ts';
    const { useAuthStore } = await import(/* @vite-ignore */ moduleUrl);
    useAuthStore().user = null;
  });
  await expect.poll(() => other.evaluate(key => localStorage.getItem(key), KEY)).toBe(saved);
  await expect(pin(page)).toHaveAttribute('aria-pressed', 'true');
  await other.reload();
  await expect(pin(other)).toHaveAttribute('aria-pressed', 'true');
  await expect(other.locator('#dashboard-sort')).toHaveValue('name-desc');
});
