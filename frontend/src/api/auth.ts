/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

import { apiClient } from './client';
import type { User, LoginRequest, RegisterRequest, AuthResponse } from '@/types';

export const authApi = {
  async register(data: RegisterRequest): Promise<AuthResponse> {
    return apiClient.post<AuthResponse>('/auth/register', data);
  },

  async login(data: LoginRequest): Promise<AuthResponse> {
    return apiClient.post<AuthResponse>('/auth/login', data);
  },

  async logout(): Promise<void> {
    await apiClient.post<void>('/auth/logout');
    apiClient.clearToken();
  },

  async getMe(): Promise<User> {
    return apiClient.get<User>('/auth/me');
  },

  async updateProfile(data: Partial<User>): Promise<User> {
    return apiClient.patch<User>('/auth/me', data);
  },

  async updatePassword(oldPassword: string, newPassword: string): Promise<void> {
    return apiClient.patch<void>('/auth/me/password', {
      old_password: oldPassword,
      new_password: newPassword,
    });
  },

  async forgotPassword(email: string): Promise<{ message: string }> {
    return apiClient.post<{ message: string }>('/auth/forgot-password', { email });
  },

  async resetPassword(token: string, newPassword: string): Promise<AuthResponse> {
    return apiClient.post<AuthResponse>('/auth/reset-password', {
      token,
      new_password: newPassword,
    });
  },

  async requestMagicLink(email: string): Promise<{ message: string }> {
    return apiClient.post<{ message: string }>('/auth/magic-link/request', { email });
  },

  /**
   * Verify a magic link.
   *
   * Verification is deliberately POST + same-origin: the GET flow let an
   * attack-site link log an already-authenticated visitor into another account.
   * The visitor confirms the link on the SPA first, and this call carries the
   * explicit intent header so a plain form post or cross-site script cannot
   * trigger a login. The server answers either a full session or, for an
   * MFA-protected account, a pending-MFA challenge with no session at all.
   */
  async verifyMagicLink(token: string): Promise<AuthResponse> {
    return apiClient.post<AuthResponse>(
      '/auth/magic-link/verify',
      { token },
      { headers: { 'X-Whento-Auth-Intent': 'magic-link' } }
    );
  },

  async checkMagicLinkAvailable(): Promise<{ available: boolean }> {
    return apiClient.get<{ available: boolean }>('/auth/magic-link/available');
  },
};
