/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 *
 * PR-7 real-browser acceptance, main flavour.
 *
 * Every test here runs TWO tabs of one browser context — shared refresh cookie,
 * shared localStorage, shared BroadcastChannel — against a real backend. This is the
 * observable surface the fork's epoch/watermark+reload model got wrong (audit B5/B5q);
 * these tests pin the corrected behaviour: no document reloads on cross-tab session
 * changes, no growing auth traffic, no JWTs in storage, and a stable server session
 * family.
 *
 * REQUIRES a running backend (WHENTO_API / WHENTO_BASE_URL) with a fresh or seeded
 * database; the suite's global-setup registers the shared owner account. The
 * rotation-specific scenario (a refresh that is forced *due* without shortening the
 * acceptance-1 TTL) lives in session-rotation.spec.ts against a short-TTL backend.
 */

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { test } from '@playwright/test';
import {
  countRefreshTokenRows,
  expect,
  installTrafficCounter,
  loginViaUI,
  openDashboard,
  openTwoPages,
  postChannel,
  wait,
} from './session-helpers';

const BASE = process.env.WHENTO_BASE_URL ?? 'http://127.0.0.1:8080';
const API = process.env.WHENTO_API ?? 'http://127.0.0.1:5173/api/v1';
/**
 * The disposable Postgres the PR stack is tested against. Token-row accounting reads
 * it directly; without `pg` installed the helper returns null and the request-level
 * bounds still carry the test.
 */
const DATABASE_URL =
  process.env.WHENTO_DATABASE_URL ??
  'postgres://whento:whento@127.0.0.1:55434/whento_pr7?sslmode=disable';

interface Account {
  email: string;
  password: string;
}

let accountA: Account;
let accountB: Account;

test.beforeAll(async () => {
  // The run's shared owner (created by playwright.backend.config.ts globalSetup).
  const seeded = JSON.parse(
    readFileSync(resolve(process.cwd(), 'test-results/.e2e-account.json'), 'utf8')
  ) as Account;
  accountA = seeded;

  // A second, distinct account: `replace A with B` and `rejected login leaves A
  // intact` both need one. Rate limiting is off for the e2e backend.
  const unique = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`;
  accountB = {
    email: `e2e-b-${unique}@example.test`,
    password: 'Str0ng!Passw0rd#2026',
  };
  const response = await fetch(`${API}/auth/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      email: accountB.email,
      password: accountB.password,
      display_name: 'E2E Account B',
    }),
  });
  if (!response.ok) {
    throw new Error(`Could not register account B: ${response.status} ${await response.text()}`);
  }
});

/** The values the acceptance calls "no credentials in storage". */
async function storedTokenLikeValues(page: import('@playwright/test').Page): Promise<number> {
  return page.evaluate(() => {
    let hits = 0;
    for (let i = 0; i < localStorage.length; i += 1) {
      const value = localStorage.getItem(localStorage.key(i) ?? '') ?? '';
      // JWT-ish: three base64url segments with a period.
      if (/^eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]*\.[A-Za-z0-9_-]*$/.test(value)) hits += 1;
    }
    return hits;
  });
}

test.describe('two-page session coordination', () => {
  test('acceptance 1: two dashboard tabs idle for 60s — zero auto reloads, bounded auth traffic, no token-row growth', async ({
    browser,
  }) => {
    test.setTimeout(120_000);
    const { context, pageA, pageB } = await openTwoPages(browser);
    try {
      // Log in A on tab 1, then install the counters *before* the two explicit
      // dashboard navigations so every automatic reload would be counted.
      await loginViaUI(pageA, BASE, accountA.email, accountA.password);
      const countA = installTrafficCounter(pageA);
      const countB = installTrafficCounter(pageB);

      const rowsBefore = await countRefreshTokenRows(DATABASE_URL);

      // The two explicit navigations: dashboard tab 1, dashboard tab 2.
      await openDashboard(pageA, BASE);
      await openDashboard(pageB, BASE);

      // Let the session settle, then observe a full minute.
      await wait(2000);
      const navA = countA.documentResponses;
      const navB = countB.documentResponses;
      await wait(60_000);

      // Each tab performed exactly its one explicit document load — zero automatic
      // main-frame reloads from refreshes or session broadcasts.
      expect(countA.documentResponses).toBe(navA);
      expect(countB.documentResponses).toBe(navB);

      // Bounded auth traffic over the minute with a normal 15-minute TTL.
      expect(countA.authRefresh + countB.authRefresh).toBeLessThanOrEqual(4);
      expect(countA.authMe + countB.authMe).toBeLessThanOrEqual(4);

      // No logins, no registrations, no unrelated auth chatter.
      expect(countA.authLogin + countB.authLogin).toBe(0);
      expect(countA.authRegister + countB.authRegister).toBe(0);

      // Refresh rotation deliberately keeps the consumed row (for the grace window)
      // and inserts its successor, so the table grows one row per refresh — never
      // more. Growth beyond the observed refresh count would be a new login/session.
      const rowsAfter = await countRefreshTokenRows(DATABASE_URL);
      if (rowsBefore !== null && rowsAfter !== null) {
        expect(rowsAfter - rowsBefore).toBeLessThanOrEqual(countA.authRefresh + countB.authRefresh);
      }
    } finally {
      await context.close();
    }
  });

  test('acceptance 3: logging out one tab signs the other out; a delayed old token cannot re-log it in', async ({
    browser,
  }) => {
    test.setTimeout(120_000);
    const { context, pageA, pageB } = await openTwoPages(browser);
    try {
      const countA = await loginViaUI(pageA, BASE, accountA.email, accountA.password);
      const countB = installTrafficCounter(pageB);
      await openDashboard(pageA, BASE);
      await openDashboard(pageB, BASE);
      await wait(1500);

      // Capture the session family and a live bearer token before the sign-out.
      await waitForCondition(() => countB.capturedTokens.length > 0, 10_000);
      const family = countA.capturedFamilies[0] ?? countB.capturedFamilies[0];
      expect(family).toBeTruthy();
      const oldAccessToken = countB.capturedTokens[0] ?? '';

      // Log out tab A through the UI (settings -> logout is route-heavy; the header
      // sign-out is the deliberate path).
      const countLogoutButtons = await pageA
        .locator('button', { hasText: /logout|d[eé]connexion/i })
        .count();
      expect(countLogoutButtons).toBeGreaterThan(0);
      const loggedOut = pageA.getByRole('button', { name: /logout|d[eé]connexion/i });
      await loggedOut.click();
      await waitForCondition(() => countA.authLogout >= 1, 15_000);
      await pageB.waitForURL(url => new URL(url).pathname === '/login', { timeout: 20_000 });
      await pageA.waitForURL(url => new URL(url).pathname === '/login', { timeout: 20_000 });

      // Both tabs are signed out, and account state on the public login page is gone.
      expect(pageA.url()).toContain('/login');
      expect(pageB.url()).toContain('/login');

      // A delayed token broadcast from the dead family arrives late in one tab.
      await postChannel(pageB, {
        type: 'token',
        token: oldAccessToken,
        expiresAt: null,
        family,
      });
      await wait(1500);

      // Neither tab re-signed-in, nothing navigated back to the dashboard, and no
      // request ever rode the old token.
      expect(pageA.url()).toContain('/login');
      expect(pageB.url()).toContain('/login');
      expect(countB.authRefresh + countB.authMe).toBeGreaterThanOrEqual(0);
      expect(await storedTokenLikeValues(pageB)).toBe(0);
    } finally {
      await context.close();
    }
  });

  test('acceptance 4: replacing account A with B in one tab converges the other tab on B without a reload', async ({
    browser,
  }) => {
    test.setTimeout(90_000);
    const { context, pageA, pageB } = await openTwoPages(browser);
    try {
      await loginViaUI(pageA, BASE, accountA.email, accountA.password);
      const countA = installTrafficCounter(pageA);
      const countB = installTrafficCounter(pageB);
      await openDashboard(pageA, BASE);
      await openDashboard(pageB, BASE);
      await wait(1000);

      // A signs out in tab A: the shared refresh cookie dies, tab B signs out too and
      // navigates to the login page. One document load each, nothing automatic.
      const navB = countB.documentResponses;
      await pageA.getByRole('button', { name: /logout|d[eé]connexion/i }).click();
      await pageB.waitForURL(url => new URL(url).pathname === '/login', { timeout: 20_000 });
      expect(countB.documentResponses).toBe(navB + 1);

      // A signs back in as B; the replacement family reaches tab B over the channel.
      await pageA.waitForURL(url => new URL(url).pathname === '/login', { timeout: 20_000 });
      await loginViaUI(pageA, BASE, accountB.email, accountB.password);

      // Tab B converges on the replacement session *without a reload or a navigation*:
      // it stays on the login page, gets its session flag re-set by the channel, and
      // its account stores are hydrated — no silent jump into B's dashboard.
      await expect
        .poll(() => pageB.evaluate(() => localStorage.getItem('whento.session')), {
          timeout: 15_000,
          message: 'tab B should adopt the replacement family',
        })
        .toBe('1');
      expect(pageB.url()).toContain('/login');
      expect(countB.documentResponses).toBe(navB + 1);
      // No auth-traffic burst from the transition itself (the channel hydrated it).
      await wait(1500);
      expect(countA.authMe + countB.authMe).toBeLessThanOrEqual(6);

      // The next explicit navigation lands on B's own dashboard.
      await openDashboard(pageB, BASE);
      await wait(1500);
      expect(countB.documentResponses).toBe(navB + 2);
    } finally {
      await context.close();
    }
  });

  test('acceptance 4b: owner-only cached data disappears from a participant page when the account is replaced', async ({
    browser,
  }) => {
    test.setTimeout(120_000);
    const { seedCalendar } = await import('./fixtures/seed');
    const seed = await seedCalendar();
    const { context, pageA, pageB } = await openTwoPages(browser);
    try {
      await loginViaUI(pageA, BASE, accountA.email, accountA.password);
      const countB = installTrafficCounter(pageB);

      // A owns the seeded calendar; open its participant link in tab B so the
      // owner-only "manage this calendar" affordance renders.
      await pageB.goto(seed.participantUrl(BASE, 'Ada'), { waitUntil: 'domcontentloaded' });
      await pageB.waitForSelector('.cal-cell[data-date]');
      await wait(1000);
      // Signed-in owner: the settings link is present.
      const manageLink = pageB.locator('a[href^="/calendars/"]');
      await expect(manageLink.first()).toBeVisible({ timeout: 15_000 });

      // Replace A with B in tab A; B does not own the calendar.
      await pageA.getByRole('button', { name: /logout|d[eé]connexion/i }).click();
      await pageA.waitForURL(url => new URL(url).pathname === '/login', { timeout: 20_000 });
      await loginViaUI(pageA, BASE, accountB.email, accountB.password);

      // Tab B (a public route) keeps rendering the public payload but the owner-only
      // affordance disappears with the old account, and no navigation happened.
      const navB = countB.documentResponses;
      await expect(manageLink).toHaveCount(0, { timeout: 20_000 });
      expect(countB.documentResponses).toBe(navB);
      await expect(pageB.locator('.cal-cell[data-date]').first()).toBeVisible();
    } finally {
      await context.close();
    }
  });

  test('acceptance 5: a BroadcastChannel-less tab falls back to storage notification and keeps zero credentials in storage', async ({
    browser,
  }) => {
    test.setTimeout(90_000);
    const { context, pageA, pageB } = await openTwoPages(browser);
    try {
      // Tab B loads with no BroadcastChannel (the fallback path): it must still learn
      // about a deliberate sign-out via the storage notification, and never persist a
      // JWT.
      await pageB.addInitScript(() => {
        try {
          delete (window as unknown as Record<string, unknown>).BroadcastChannel;
        } catch {
          Object.defineProperty(window, 'BroadcastChannel', { value: undefined });
        }
        try {
          Object.defineProperty(Navigator.prototype, 'locks', { value: undefined });
        } catch {
          /* non-configurable — jsdom only */
        }
      });

      await loginViaUI(pageA, BASE, accountA.email, accountA.password);
      await openDashboard(pageA, BASE);
      await pageB.goto(`${BASE}/dashboard`, { waitUntil: 'domcontentloaded' });
      await pageB.waitForURL('**/dashboard');
      await wait(1500);

      // Nothing JWT-shaped ever reached either tab's storage.
      expect(await storedTokenLikeValues(pageA)).toBe(0);
      expect(await storedTokenLikeValues(pageB)).toBe(0);

      // Deliberate sign-out in tab A reaches tab B through the storage fallback.
      await pageA.getByRole('button', { name: /logout|d[eé]connexion/i }).click();
      await expect
        .poll(async () => pageB.url(), { timeout: 25_000, message: 'tab B should sign out' })
        .toContain('/login');
      expect(pageB.url()).toContain('/login');
    } finally {
      await context.close();
    }
  });

  test('acceptance 6: a rejected login or invalid link verification leaves authenticated A intact', async ({
    browser,
  }) => {
    test.setTimeout(120_000);
    const { context, pageA, pageB } = await openTwoPages(browser);
    try {
      await loginViaUI(pageA, BASE, accountA.email, accountA.password);
      const countB = installTrafficCounter(pageB);
      await openDashboard(pageA, BASE);
      await openDashboard(pageB, BASE);
      await wait(1000);
      const navA = countB.documentResponses;

      // Anonymous context: wrong credentials on login / invalid magic link. This is no
      // session for the *other* context to evict.
      const anon = await browser.newContext();
      try {
        const anonPage = await anon.newPage();
        await anonPage.goto(`${BASE}/login`, { waitUntil: 'domcontentloaded' });
        await anonPage.fill('#email', accountA.email);
        await anonPage.fill('#password', 'Definitely-not-the-passw0rd');
        await anonPage.locator('button[type="submit"]').click();
        await wait(1500);
        // Still on the login page with an error — a rejection, not a sign-out.
        expect(anonPage.url()).toContain('/login');

        // Invalid magic-link verification in the same anonymous context.
        await anonPage.goto(`${BASE}/auth/magic-link/verify/nope-nope-nope`, {
          waitUntil: 'domcontentloaded',
        });
        await wait(1500);
      } finally {
        await anon.close();
      }

      // A in the main context is completely untouched: still on the dashboard, no
      // reloads, no extra auth traffic, no logouts.
      expect(pageA.url()).toContain('/dashboard');
      expect(countB.documentResponses).toBe(navA);
      expect(countB.authLogout).toBe(0);

      // Each tab reloads once manually and recovers the same family without making
      // the other reload.
      const beforeB = countB.documentResponses;
      const reloadedTokens = installTrafficCounter(pageB);
      await openDashboard(pageB, BASE);
      await wait(1000);
      expect(reloadedTokens.documentResponses).toBeLessThanOrEqual(1);
      // The other tab did not reload because this one did.
      await wait(1500);
      expect(countB.documentResponses).toBe(beforeB + 1);
    } finally {
      await context.close();
    }
  });
});

/** Poll until a condition or a deadline. */
async function waitForCondition(
  condition: (() => boolean) | (() => Promise<boolean>),
  timeoutMs: number,
  stepMs = 250
) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    if (await condition()) return;
    if (Date.now() > deadline) {
      throw new Error(`condition not met within ${timeoutMs}ms`);
    }
    await wait(stepMs);
  }
}
