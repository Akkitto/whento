/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { ref } from 'vue';

import { authApi } from '@/api/auth';

/**
 * The SMTP capability probe is deliberately not a boolean. Whether the email
 * channel can currently be delivered is separate from the owner's saved
 * email.enabled setting. None of these states rewrites that setting:
 *
 * - `available` — the instance has SMTP; the email channel may be offered.
 * - `unavailable` — the instance has no SMTP; newly enabling email is not
 *   offered. Existing saved intent survives until SMTP is restored.
 * - `error` — the probe itself failed. That is a retryable warning, never a
 *   reason to destroy a saved email.enabled flag.
 * - `unknown` — the probe has not answered yet (still in flight, or never run).
 *
 * Capability never rewrites saved consent. Only an explicit owner edit changes
 * email.enabled; the backend gates enqueueing and delivery on SMTP availability.
 */
export type SmtpProbeState = 'unknown' | 'available' | 'unavailable' | 'error';

/** useSmtpProbe reads the instance's SMTP capability through the existing
 * /auth/magic-link/available endpoint (which answers emailService.IsConfigured).
 */
export function useSmtpProbe() {
  const state = ref<SmtpProbeState>('unknown');
  let generation = 0;

  const probe = async (): Promise<SmtpProbeState> => {
    const request = ++generation;
    state.value = 'unknown';
    try {
      const result = await authApi.checkMagicLinkAvailable();
      if (request === generation) {
        state.value = result.available ? 'available' : 'unavailable';
      }
    } catch {
      if (request === generation) {
        state.value = 'error';
      }
    }
    return state.value;
  };

  return { state, probe };
}
