/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 * @vitest-environment jsdom
 */

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia } from 'pinia';
import { mountWithI18n } from '@/test/harness';
import { addParticipantEmail, resendVerificationEmail } from '@/api/notify';
import ParticipantEmailPanel from './ParticipantEmailPanel.vue';

vi.mock('@/api/notify', () => ({
  addParticipantEmail: vi.fn(),
  resendVerificationEmail: vi.fn(),
}));

function mountPanel(props: Record<string, unknown> = {}) {
  return mountWithI18n(ParticipantEmailPanel, {
    props: { token: 'calendar-token', participantId: 'participant', ...props },
    global: { plugins: [createPinia()] },
  });
}

describe('ParticipantEmailPanel capability', () => {
  beforeEach(() => vi.clearAllMocks());

  it.each(['unknown', 'unavailable', 'error'] as const)(
    'keeps verified email visible while SMTP is %s without allowing sends',
    async smtpProbe => {
      const wrapper = mountPanel({ smtpProbe, email: 'ada@example.test', emailVerified: true });
      expect(wrapper.text()).toContain('ada@example.test');
      expect(wrapper.text()).toContain('verified');
      const change = wrapper.findAll('button').find(button => button.text().includes('Change'))!;
      expect((change.element as HTMLButtonElement).disabled).toBe(true);
      await change.trigger('click');
      expect(wrapper.find('form').exists()).toBe(false);
      expect(addParticipantEmail).not.toHaveBeenCalled();
      wrapper.unmount();
    }
  );

  it.each(['unknown', 'unavailable', 'error'] as const)(
    'keeps pending email visible while SMTP is %s and disables resend',
    async smtpProbe => {
      const wrapper = mountPanel({ smtpProbe, email: 'ada@example.test', emailVerified: false });
      expect(wrapper.text()).toContain('ada@example.test');
      const resend = wrapper.findAll('button').find(button => button.text().includes('Resend'))!;
      expect((resend.element as HTMLButtonElement).disabled).toBe(true);
      await resend.trigger('click');
      expect(resendVerificationEmail).not.toHaveBeenCalled();
      wrapper.unmount();
    }
  );

  it('offers a retry after a failed probe, then restores the enrolment form', async () => {
    const wrapper = mountPanel({ smtpProbe: 'error' });
    expect(wrapper.text()).toContain('Could not verify email availability');
    expect(wrapper.find('form').exists()).toBe(false);
    await wrapper.find('button').trigger('click');
    expect(wrapper.emitted('retry-smtp-probe')).toEqual([[]]);
    await wrapper.setProps({ smtpProbe: 'available' });
    expect(wrapper.find('form').exists()).toBe(true);
    expect(wrapper.find('[role="status"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it('shows existing email read-only if the owner revoked participant mail', () => {
    const wrapper = mountPanel({
      smtpProbe: 'available',
      emailAllowed: false,
      email: 'ada@example.test',
      emailVerified: true,
    });
    expect(wrapper.text()).toContain('ada@example.test');
    expect(wrapper.text()).toContain('owner has disabled');
    expect((wrapper.find('button').element as HTMLButtonElement).disabled).toBe(true);
    wrapper.unmount();
  });

  it('cannot submit a change form that was opened before SMTP became unavailable', async () => {
    const wrapper = mountPanel({
      smtpProbe: 'available',
      email: 'ada@example.test',
      emailVerified: true,
    });
    await wrapper.find('button').trigger('click');
    expect(wrapper.find('form').exists()).toBe(true);
    await wrapper.setProps({ smtpProbe: 'unavailable' });
    expect(wrapper.find('form').exists()).toBe(false);
    expect(wrapper.text()).toContain('ada@example.test');
    expect(addParticipantEmail).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
