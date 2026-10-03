/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { ref } from 'vue';

import { authApi } from '@/api/auth';
import type { NotifyConfig } from '@/api/notify';

/**
 * The SMTP capability probe is deliberately not a boolean. Whether the email
 * channel can ever be delivered is a three-valued question, and two of those
 * values must not touch the owner's saved email.enabled setting:
 *
 * - `available` — the instance has SMTP; the email channel may be offered.
 * - `unavailable` — the instance has no SMTP; the email channel must not be
 *   offered, and persisting it disabled is truthful.
 * - `error` — the probe itself failed. That is a retryable warning, never a
 *   reason to destroy a saved email.enabled flag.
 * - `unknown` — the probe has not answered yet (still in flight, or never run).
 *
 * Only a confirmed `unavailable` answer may rewrite email.enabled, because only
 * that case knows for certain the mailbox could never receive mail.
 */
export type SmtpProbeState = 'unknown' | 'available' | 'unavailable' | 'error';

/** useSmtpProbe reads the instance's SMTP capability through the existing
 * /auth/magic-link/available endpoint (which answers emailService.IsConfigured).
 */
export function useSmtpProbe() {
  const state = ref<SmtpProbeState>('unknown');

  const probe = async (): Promise<SmtpProbeState> => {
    state.value = 'unknown';
    try {
      const result = await authApi.checkMagicLinkAvailable();
      state.value = result.available ? 'available' : 'unavailable';
    } catch {
      state.value = 'error';
    }
    return state.value;
  };

  return { state, probe };
}

/**
 * Returns the config that should actually be persisted for the given probe
 * outcome. Only confirmed `unavailable` forces the email channel off; `unknown`
 * and `error` preserve whatever the owner saved, so a failed probe — or an
 * unrelated settings save — cannot silently clobber email.enabled.
 */
export function applySmtpProbeToConfig(
  config: NotifyConfig,
  probeState: SmtpProbeState
): NotifyConfig {
  const next: NotifyConfig = {
    ...config,
    channels: { ...config.channels, email: { ...config.channels.email } },
  };
  if (probeState === 'unavailable') {
    next.channels.email.enabled = false;
  }
  return next;
}
