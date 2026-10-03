/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { beforeEach, describe, expect, it, vi } from 'vitest';

import { authApi } from '@/api/auth';
import type { NotifyConfig } from '@/api/notify';

import { applySmtpProbeToConfig, useSmtpProbe } from './smtpProbe';

vi.mock('@/api/auth', () => ({ authApi: { checkMagicLinkAvailable: vi.fn() } }));

function savedConfig(): NotifyConfig {
  return {
    calendar_id: 'cal-1',
    reminders: { enabled: true, hours_before: 24 },
    channels: {
      email: { enabled: true, recipient: 'owner' },
      chat: { enabled: true, recipient: 'owner' },
    },
  } as unknown as NotifyConfig;
}

describe('useSmtpProbe', () => {
  beforeEach(() => {
    vi.mocked(authApi.checkMagicLinkAvailable).mockReset();
  });

  it('starts unknown', () => {
    const { state } = useSmtpProbe();
    expect(state.value).toBe('unknown');
  });

  it('reports available when SMTP is configured', async () => {
    vi.mocked(authApi.checkMagicLinkAvailable).mockResolvedValue({ available: true });
    const { state, probe } = useSmtpProbe();

    await expect(probe()).resolves.toBe('available');
    expect(state.value).toBe('available');
  });

  it('reports unavailable when SMTP is absent', async () => {
    vi.mocked(authApi.checkMagicLinkAvailable).mockResolvedValue({ available: false });
    const { state, probe } = useSmtpProbe();

    await expect(probe()).resolves.toBe('unavailable');
    expect(state.value).toBe('unavailable');
  });

  it('reports error when the probe request fails (retryable warning, not unavailable)', async () => {
    vi.mocked(authApi.checkMagicLinkAvailable).mockRejectedValue(new Error('network down'));
    const { state, probe } = useSmtpProbe();

    await expect(probe()).resolves.toBe('error');
    expect(state.value).toBe('error');
  });
});

describe('applySmtpProbeToConfig', () => {
  it('preserves saved email.enabled when the probe is unknown or errored', () => {
    for (const state of ['unknown', 'error'] as const) {
      const next = applySmtpProbeToConfig(savedConfig(), state);
      expect(next.channels.email.enabled).toBe(true);
      expect(next.channels.chat.enabled).toBe(true);
    }
  });

  it('disables the email channel only on a confirmed unavailable answer', () => {
    const next = applySmtpProbeToConfig(savedConfig(), 'unavailable');
    expect(next.channels.email.enabled).toBe(false);
  });

  it('does not touch any other channel on any outcome', () => {
    for (const state of ['unknown', 'available', 'unavailable', 'error'] as const) {
      const next = applySmtpProbeToConfig(savedConfig(), state);
      expect(next.channels.chat.enabled).toBe(true);
    }
  });
});
