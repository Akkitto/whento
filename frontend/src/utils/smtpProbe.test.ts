/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { beforeEach, describe, expect, it, vi } from 'vitest';

import { authApi } from '@/api/auth';
import { useSmtpProbe } from './smtpProbe';

vi.mock('@/api/auth', () => ({ authApi: { checkMagicLinkAvailable: vi.fn() } }));

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

  it.each(['unavailable', 'error'] as const)(
    'ignores an older %s response after a newer successful probe',
    async olderOutcome => {
      let resolveOld!: (value: { available: boolean }) => void;
      let rejectOld!: (error: Error) => void;
      const oldRequest = new Promise<{ available: boolean }>((resolve, reject) => {
        resolveOld = resolve;
        rejectOld = reject;
      });
      vi.mocked(authApi.checkMagicLinkAvailable)
        .mockReturnValueOnce(oldRequest)
        .mockResolvedValueOnce({ available: true });
      const { state, probe } = useSmtpProbe();
      const first = probe();
      await probe();
      if (olderOutcome === 'error') rejectOld(new Error('stale request failed'));
      else resolveOld({ available: false });
      await first;
      expect(state.value).toBe('available');
    }
  );
});
