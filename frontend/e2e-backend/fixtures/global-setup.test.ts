/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { writeFileSync } from 'node:fs';
import { apiFetch } from './api';
import globalSetup from './global-setup';

vi.mock('node:fs', () => ({ mkdirSync: vi.fn(), writeFileSync: vi.fn() }));
vi.mock('./api', () => ({
  ACCOUNT_FILE: '/test-results/account.json',
  API: 'http://backend.test/api/v1',
  apiFetch: vi.fn(),
}));

describe('backend E2E account setup', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    vi.stubEnv('BOOTSTRAP_KEY', 'synthetic-key-matching-the-test-server');
    vi.stubEnv('WHENTO_BOOTSTRAP_KEY', undefined);
  });

  afterEach(() => vi.unstubAllEnvs());

  it('bootstraps the initial verified owner on a fresh instance', async () => {
    vi.mocked(apiFetch)
      .mockResolvedValueOnce({ needs_bootstrap: true })
      .mockResolvedValueOnce({})
      .mockResolvedValueOnce({ access_token: 'test-token' });

    await globalSetup();

    expect(apiFetch).toHaveBeenNthCalledWith(1, '/auth/status');
    expect(apiFetch).toHaveBeenNthCalledWith(2, '/auth/bootstrap', {
      method: 'POST',
      body: expect.objectContaining({ boot_key: 'synthetic-key-matching-the-test-server' }),
    });
    expect(apiFetch).toHaveBeenNthCalledWith(3, '/auth/login', expect.any(Object));
    expect(writeFileSync).toHaveBeenCalledWith(
      '/test-results/account.json',
      expect.stringContaining('test-token')
    );
  });

  it('uses registration without exposing the key on an already-configured instance', async () => {
    vi.mocked(apiFetch)
      .mockResolvedValueOnce({ needs_bootstrap: false })
      .mockResolvedValueOnce({})
      .mockResolvedValueOnce({ access_token: 'test-token' });

    await globalSetup();

    expect(apiFetch).toHaveBeenNthCalledWith(2, '/auth/register', {
      method: 'POST',
      body: {
        email: expect.any(String),
        password: expect.any(String),
        display_name: 'E2E Seeder',
      },
    });
  });

  it('fails before attempting registration when a fresh instance has no test key', async () => {
    vi.stubEnv('BOOTSTRAP_KEY', undefined);
    vi.mocked(apiFetch).mockResolvedValueOnce({ needs_bootstrap: true });

    await expect(globalSetup()).rejects.toThrow('matching the server');
    expect(apiFetch).toHaveBeenCalledTimes(1);
    expect(writeFileSync).not.toHaveBeenCalled();
  });

  it('propagates a failed status read without guessing the bootstrap state', async () => {
    vi.mocked(apiFetch).mockRejectedValueOnce(new Error('status unavailable'));

    await expect(globalSetup()).rejects.toThrow('status unavailable');
    expect(apiFetch).toHaveBeenCalledTimes(1);
  });
});
