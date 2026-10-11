/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { expect, test } from '@playwright/test';

test('French feed membership is usable in all layouts and pointer grips are not tab stops', async ({
  page,
}) => {
  const apiRequests: string[] = [];
  page.on('request', request => {
    if (new URL(request.url()).pathname.startsWith('/api/')) apiRequests.push(request.url());
  });
  await page.goto('/dev/preview-dashboard.html?lang=fr&feed=1');
  await page.selectOption('#dashboard-sort', 'custom');
  for (const view of ['card', 'expanded', 'list', 'compact']) {
    await page.click(`[data-action="view-${view}"]`);
    await expect(page.locator('article input[data-action="feed"]')).toHaveCount(4);
    const card = page.locator('article[data-calendar-id="alpha"]');
    const feed = card.getByRole('checkbox', { name: 'Inclure dans le flux' });
    await feed.check();
    await expect(feed).toBeChecked();
    await feed.uncheck();
    await expect(feed).not.toBeChecked();
    await expect(card.locator('[data-action="drag"]')).toHaveAttribute('tabindex', '-1');
    await expect(card.locator('[data-action="drag"]')).toHaveAttribute('aria-hidden', 'true');
    await expect(card.getByRole('button', { name: 'Monter', exact: true })).toBeDisabled();
    await expect(page).toHaveURL(/preview-dashboard\.html/);
  }
  expect(apiRequests).toEqual([]);
});
