/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { expect, test } from '@playwright/test';
import { addAvailability, fetchAvailabilities, seedCalendar } from './fixtures/seed';

// The calendar/range routes use the real backend. Only the participant's private
// email fixture and the SMTP capability response are overridden; this exercises
// ParticipantView's wiring to the panel without depending on external SMTP.
test('failed SMTP probe keeps verified email visible and retry restores actions', async ({
  page,
  baseURL,
}) => {
  const seed = await seedCalendar();
  await page.route(`**/api/v1/calendars/public/${seed.publicToken}*`, async route => {
    const response = await route.fetch();
    const payload = await response.json();
    payload.data.notify_participants = true;
    const participant = payload.data.participants.find(
      (p: { id: string }) => p.id === seed.participants.Ada.id
    );
    participant.email = 'ada@example.test';
    participant.email_verified = true;
    await route.fulfill({ response, json: payload });
  });
  let probes = 0;
  await page.route('**/api/v1/auth/magic-link/available', async route => {
    probes++;
    if (probes === 1) await route.abort('failed');
    else await route.fulfill({ json: { data: { available: true } } });
  });
  await page.goto(seed.participantUrl(baseURL!, 'Ada'));
  await expect(page.getByText('ada@example.test', { exact: false })).toBeVisible();
  await expect(
    page.getByText('Could not verify email availability', { exact: false })
  ).toBeVisible();
  const change = page.getByRole('button', { name: 'Change email', exact: true });
  await expect(change).toBeDisabled();
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(change).toBeEnabled();
  await expect(page.getByText('Could not verify email availability', { exact: false })).toHaveCount(
    0
  );
  expect(probes).toBe(2);
});

test('participant reminder cancellation link removes only the chosen event date', async ({
  page,
  baseURL,
}) => {
  const seed = await seedCalendar();
  const canceledDate = seed.day(0);
  await addAvailability(seed.publicToken, seed.participants.Ada.id, seed.day(8));
  await page.goto(`${seed.participantUrl(baseURL!, 'Ada')}?cancel=${canceledDate}`);
  await expect(
    page.getByText(`Your participation has been cancelled for ${canceledDate}`, { exact: true })
  ).toBeVisible();
  await expect(page).not.toHaveURL(/cancel=/);
  const stored = await fetchAvailabilities(
    seed.publicToken,
    seed.participants.Ada.id,
    seed.day(0),
    seed.day(20)
  );
  expect(stored.map(a => a.date)).not.toContain(canceledDate);
  expect(stored.map(a => a.date)).toContain(seed.day(8));
});
