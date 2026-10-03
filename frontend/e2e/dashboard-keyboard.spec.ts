/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { expect, test, type Page } from '@playwright/test';

const PREVIEW = '/dev/preview-dashboard.html';

/**
 * Focus regression (S3): moving a dashboard card with the keyboard must leave focus
 * on the moved card's own control, never on BODY. Driven against the real Dashboard
 * rendered on fabricated data (dev/preview-dashboard.ts), so these are genuine
 * keyboard interactions in a real browser — not component-level synthesis.
 */

/** The focused control, described by its stable data markers (null when BODY). */
async function focused(page: Page) {
  return page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    if (!el || el === document.body) return null;
    return {
      tag: el.tagName,
      action: el.getAttribute('data-action'),
      calendar: el.closest('[data-calendar-id]')?.getAttribute('data-calendar-id') ?? null,
      ring:
        el.classList.contains('focus-visible-fallback') ||
        (() => {
          try {
            return el.matches(':focus-visible');
          } catch {
            return false;
          }
        })(),
    };
  });
}

/** Move the given card in the given direction with the keyboard (focus + Enter). */
async function moveWithKeyboard(page: Page, calendarId: string, direction: 'up' | 'down') {
  const control = page.locator(
    `article[data-calendar-id="${calendarId}"] button[data-action="move-${direction}"]`
  );
  await control.focus();
  await page.keyboard.press('Enter');
}

async function orderOnPage(page: Page) {
  return page.$$eval('article[data-calendar-id]', articles =>
    articles.map(a => a.getAttribute('data-calendar-id')!)
  );
}

async function startCustomOrder(page: Page) {
  await page.goto(PREVIEW);
  await expect(page.locator('#dashboard-sort')).toHaveCount(1, { timeout: 10_000 });
  await page.selectOption('#dashboard-sort', 'custom');
  await expect(page.locator('article[data-calendar-id]')).toHaveCount(4);
}

test.describe('dashboard keyboard reordering — list view', () => {
  test('a middle move keeps focus on the moved card move control, never BODY', async ({ page }) => {
    await startCustomOrder(page);
    await page.click('button[data-action="view-list"]');

    // Alphabetical seed: [alpha, bravo, charlie, delta]. "Charlie" (index 2) moves up
    // to index 1; its move-up button stays enabled, so focus must land back on it.
    await moveWithKeyboard(page, 'charlie', 'up');

    await expect
      .poll(async () => orderOnPage(page))
      .toEqual(['alpha', 'charlie', 'bravo', 'delta']);
    await expect
      .poll(() => focused(page))
      .toEqual({
        tag: 'BUTTON',
        action: 'move-up',
        calendar: 'charlie',
        ring: true,
      });
  });

  test('repeated moves keep focus, falling to the opposite control at the top edge', async ({
    page,
  }) => {
    await startCustomOrder(page);
    await page.click('button[data-action="view-list"]');

    // First move middle → focus on charlie's move-up.
    await moveWithKeyboard(page, 'charlie', 'up');
    await expect.poll(() => focused(page)).toMatchObject({ calendar: 'charlie' });

    // Second immediate move pushes "Charlie" to the top: its move-up is now dead, so
    // focus must transfer to the opposite enabled control ("move down"), still on the
    // moved card, still showing a focus ring.
    await page.keyboard.press('Enter');
    await expect
      .poll(async () => orderOnPage(page))
      .toEqual(['charlie', 'alpha', 'bravo', 'delta']);
    await expect
      .poll(() => focused(page))
      .toEqual({
        tag: 'BUTTON',
        action: 'move-down',
        calendar: 'charlie',
        ring: true,
      });
  });
});

test.describe('dashboard keyboard reordering — compact view', () => {
  test('a middle move keeps focus on the moved card move control, never BODY', async ({ page }) => {
    await startCustomOrder(page);
    await page.click('button[data-action="view-compact"]');

    await moveWithKeyboard(page, 'charlie', 'up');

    await expect
      .poll(async () => orderOnPage(page))
      .toEqual(['alpha', 'charlie', 'bravo', 'delta']);
    await expect
      .poll(() => focused(page))
      .toEqual({
        tag: 'BUTTON',
        action: 'move-up',
        calendar: 'charlie',
        ring: true,
      });
  });

  test('a boundary move lands on the opposite enabled control of the moved card', async ({
    page,
  }) => {
    await startCustomOrder(page);
    await page.click('button[data-action="view-compact"]');

    // Move "Bravo" (index 1) up to the top of the group; moving up from there is
    // impossible, so focus falls to its "move down" control.
    await moveWithKeyboard(page, 'bravo', 'up');
    await expect
      .poll(async () => orderOnPage(page))
      .toEqual(['bravo', 'alpha', 'charlie', 'delta']);
    await expect
      .poll(() => focused(page))
      .toEqual({
        tag: 'BUTTON',
        action: 'move-down',
        calendar: 'bravo',
        ring: true,
      });
  });

  test('the live region announces the new position after a move', async ({ page }) => {
    await startCustomOrder(page);
    await page.click('button[data-action="view-list"]');

    await moveWithKeyboard(page, 'delta', 'up');
    const live = page.locator('#dashboard-move-live');
    await expect(live).toHaveAttribute('aria-live', 'polite');
    await expect(live).toContainText('position 3');
  });
});
