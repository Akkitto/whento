/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 * @vitest-environment jsdom
 */

import { describe, expect, it } from 'vitest';
import { getDefaultNotifyConfig } from '@/api/notify';
import { lastEmit, mountWithI18n } from '@/test/harness';
import NotificationSettings from './NotificationSettings.vue';

describe('NotificationSettings SMTP consent', () => {
  it('allows explicit disable of saved email consent during an SMTP outage', async () => {
    const config = getDefaultNotifyConfig();
    config.enabled = true;
    config.channels.email.enabled = true;
    const wrapper = mountWithI18n(NotificationSettings, {
      props: { modelValue: config, smtpProbe: 'unavailable' },
    });
    const checkbox = wrapper.find('#channel-email');
    expect(checkbox.exists()).toBe(true);
    expect((checkbox.element as HTMLInputElement).checked).toBe(true);
    await checkbox.setValue(false);
    expect(lastEmit(wrapper, 'update:modelValue')?.[0]).toMatchObject({
      channels: { email: { enabled: false } },
    });
    expect(config.channels.email.enabled).toBe(true);
    wrapper.unmount();
  });

  it('does not offer a new email opt-in while SMTP is confirmed unavailable', () => {
    const config = getDefaultNotifyConfig();
    config.enabled = true;
    config.channels.email.enabled = false;
    const wrapper = mountWithI18n(NotificationSettings, {
      props: { modelValue: config, smtpProbe: 'unavailable' },
    });
    expect(wrapper.find('#channel-email').exists()).toBe(false);
    wrapper.unmount();
  });
});
