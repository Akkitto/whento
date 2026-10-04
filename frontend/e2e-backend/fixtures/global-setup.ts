/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { ACCOUNT_FILE, API, apiFetch } from './api';

/**
 * Bootstrap a fresh instance, or register one account on a configured instance.
 *
 * This cannot be done per test. `/auth/register` is rate limited to 3 requests per
 * minute per IP and `/auth/login` to 5, so a suite that registered per test would 429
 * on its fourth case — which is exactly how the first version of this suite failed.
 * One account is created here, its token written to disk, and every worker reads it.
 *
 * Calendars are still created per test, so tests remain independent: they share an
 * owner, never a calendar.
 */
export default async function globalSetup(): Promise<void> {
  const unique = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`;
  const email = `e2e-${unique}@example.test`;
  const password = 'Str0ng!Passw0rd#2026';

  const status = await apiFetch<{ needs_bootstrap: boolean }>('/auth/status');
  const bootKey = process.env.WHENTO_BOOTSTRAP_KEY ?? process.env.BOOTSTRAP_KEY;
  if (status.needs_bootstrap && !bootKey) {
    throw new Error(
      'Fresh backend E2E fixtures require WHENTO_BOOTSTRAP_KEY or BOOTSTRAP_KEY matching the server.'
    );
  }

  await apiFetch(status.needs_bootstrap ? '/auth/bootstrap' : '/auth/register', {
    method: 'POST',
    body: {
      email,
      password,
      display_name: 'E2E Seeder',
      ...(status.needs_bootstrap ? { boot_key: bootKey } : {}),
    },
  });

  const login = await apiFetch<{ access_token: string }>('/auth/login', {
    method: 'POST',
    body: { email, password },
  });

  mkdirSync(dirname(ACCOUNT_FILE), { recursive: true });
  writeFileSync(
    ACCOUNT_FILE,
    JSON.stringify({ email, password, accessToken: login.access_token, api: API }, null, 2)
  );
}
