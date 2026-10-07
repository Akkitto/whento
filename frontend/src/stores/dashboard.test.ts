/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 *
 * @vitest-environment jsdom
 */

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';
import { nextTick } from 'vue';
import type { User } from '@/types';

const authApi = {
  login: vi.fn(),
  register: vi.fn(),
  bootstrap: vi.fn(),
  bootstrapStatus: vi.fn(),
  getMe: vi.fn(),
  updateProfile: vi.fn(),
  updatePassword: vi.fn(),
  logout: vi.fn(),
  forgotPassword: vi.fn(),
  resetPassword: vi.fn(),
  checkMagicLinkAvailable: vi.fn(),
};

const apiClient = {
  setToken: vi.fn(),
  clearToken: vi.fn(),
  hasSession: vi.fn(() => false),
  signOut: vi.fn(),
};

vi.mock('@/api/auth', () => ({ authApi }));
vi.mock('@/api/client', () => ({ apiClient }));

const { useAuthStore } = await import('./auth');
const { useDashboardStore } = await import('./dashboard');

function user(id: string): User {
  return { id, email: `${id}@x.test`, display_name: id, role: 'user' } as User;
}

function stored(userId: string | null): unknown {
  const key = `dashboard:${userId ?? 'anon'}:prefs`;
  return JSON.parse(localStorage.getItem(key) ?? '{}');
}

describe('dashboard preferences store', () => {
  beforeEach(() => {
    localStorage.clear();
    setActivePinia(createPinia());
  });

  it('starts with sane defaults', () => {
    const store = useDashboardStore();
    expect(store.sortMode).toBe('alphabetical');
    expect(store.sortDirection).toBe('asc');
    expect(store.viewMode).toBe('card');
    expect(store.pinnedIds).toEqual([]);
    expect(store.customOrder).toEqual([]);
  });

  it('persists changes across store instances', async () => {
    useAuthStore().user = user('u-1');
    const store = useDashboardStore();
    store.togglePin('c-1');
    store.setViewMode('compact');
    store.setSortMode('custom');
    store.setCustomOrder(['c-2', 'c-1']);
    // The persistence happens synchronously at mutation time.
    await nextTick();

    setActivePinia(createPinia());
    useAuthStore().user = user('u-1');
    const reloaded = useDashboardStore();
    expect(reloaded.pinnedIds).toEqual(['c-1']);
    expect(reloaded.viewMode).toBe('compact');
    expect(reloaded.sortMode).toBe('custom');
    expect(reloaded.customOrder).toEqual(['c-2', 'c-1']);
  });

  it('toggles pins both ways', () => {
    const store = useDashboardStore();
    store.togglePin('c-1');
    store.togglePin('c-2');
    expect(store.pinnedIds).toEqual(['c-1', 'c-2']);
    store.togglePin('c-1');
    expect(store.pinnedIds).toEqual(['c-2']);
  });

  it('appends unknown ids to the custom order, but not duplicates', () => {
    const store = useDashboardStore();
    store.setCustomOrder(['c-1']);
    store.appendToCustomOrder(['c-2', 'c-1']);
    expect(store.customOrder).toEqual(['c-1', 'c-2']);
  });

  it('prunes deleted calendar ids from the custom order', () => {
    const store = useDashboardStore();
    store.setCustomOrder(['c-1', 'c-2', 'c-3']);
    store.pruneCustomOrder(['c-1', 'c-3']);
    expect(store.customOrder).toEqual(['c-1', 'c-3']);
  });

  it('discards a malformed stored value and falls back to defaults', () => {
    localStorage.setItem(
      'dashboard:anon:prefs',
      JSON.stringify({ sortMode: 'sideways', viewMode: 'globe', pinnedIds: 'not-an-array' })
    );
    setActivePinia(createPinia());
    const store = useDashboardStore();
    expect(store.sortMode).toBe('alphabetical');
    expect(store.viewMode).toBe('card');
    expect(store.pinnedIds).toEqual([]);
  });

  it('survives a truncated or invalid JSON blob', () => {
    localStorage.setItem('dashboard:anon:prefs', '{not json');
    setActivePinia(createPinia());
    const store = useDashboardStore();
    expect(store.viewMode).toBe('card');
    expect(store.sortMode).toBe('alphabetical');
  });

  it('survives a foreign-shaped stored value (not an object)', () => {
    localStorage.setItem('dashboard:anon:prefs', '"just a string"');
    setActivePinia(createPinia());
    const store = useDashboardStore();
    expect(store.sortMode).toBe('alphabetical');
    expect(store.pinnedIds).toEqual([]);
  });

  it('keeps working when localStorage is denied, degrading gracefully', () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError');
    });
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError');
    });

    try {
      setActivePinia(createPinia());
      const store = useDashboardStore();
      expect(store.sortMode).toBe('alphabetical');
      expect(store.viewMode).toBe('card');

      // Mutations must not throw even though nothing can be persisted.
      expect(() => {
        store.togglePin('c-1');
        store.setViewMode('list');
        store.setCustomOrder(['c-1']);
      }).not.toThrow();
      expect(store.pinnedIds).toEqual(['c-1']);
      expect(store.viewMode).toBe('list');
    } finally {
      getItem.mockRestore();
      setItem.mockRestore();
    }
  });

  it('deduplicates duplicated pin and order ids from a corrupted fixture', () => {
    // Old/legacy storage can hold duplicate ids; each calendar must render once, and
    // empty ids are meaningless.
    localStorage.setItem(
      'dashboard:anon:prefs',
      JSON.stringify({
        sortMode: 'custom',
        pinnedIds: ['c-a1', 'c-a1', 'c-b1', ''],
        customOrder: ['c-b1', 'c-a1', 'c-a1', '', 'c-b1'],
      })
    );
    const store = useDashboardStore();
    expect(store.pinnedIds).toEqual(['c-a1', 'c-b1']);
    expect(store.customOrder).toEqual(['c-b1', 'c-a1']);

    // The next write persists the cleaned lists, not the corrupted ones.
    store.setSortMode('alphabetical');
    const saved = stored(null) as { pinnedIds?: unknown; customOrder?: unknown };
    expect(saved.pinnedIds).toEqual(['c-a1', 'c-b1']);
    expect(saved.customOrder).toEqual(['c-b1', 'c-a1']);
  });

  it("keeps each account's pins, order and view separate", async () => {
    const authStore = useAuthStore();
    authStore.user = user('u-a');
    const store = useDashboardStore();
    store.togglePin('c-a1');
    store.setCustomOrder(['c-a1']);
    store.setViewMode('compact');
    await nextTick();

    // Account B signs in: the store reloads (empty) preferences scoped to B.
    authStore.user = user('u-b');
    await nextTick();
    expect(store.pinnedIds).toEqual([]);
    expect(store.customOrder).toEqual([]);
    expect(store.viewMode).toBe('card');

    // B writes its own data; A's saved data is untouched, including the view.
    store.togglePin('c-b1');
    await nextTick();

    // A signs back in: their pins, order and compact view return.
    authStore.user = user('u-a');
    await nextTick();
    expect(store.pinnedIds).toEqual(['c-a1']);
    expect(store.customOrder).toEqual(['c-a1']);
    expect(store.viewMode).toBe('compact');

    // And B's persisted prefs are still exactly what B saved.
    setActivePinia(createPinia());
    const bAuth = useAuthStore();
    bAuth.user = user('u-b');
    const b = useDashboardStore();
    expect(b.pinnedIds).toEqual(['c-b1']);
    expect(b.viewMode).toBe('card');
  });

  it("does not save A's changes into B's key when the account switches in the same tick", async () => {
    const authStore = useAuthStore();
    authStore.user = user('u-a');
    const store = useDashboardStore();
    store.togglePin('c-a1');

    // The switch lands before Vue flushes the persistence watcher, so the in-memory
    // value A just set is still pending when the user id changes.
    authStore.user = user('u-b');
    await nextTick();

    const aStored = stored('u-a') as { pinnedIds?: unknown };
    const bStored = stored('u-b') as { pinnedIds?: unknown };
    expect(aStored.pinnedIds).toEqual(['c-a1']);
    expect(bStored.pinnedIds).toBeUndefined();
    expect(store.pinnedIds).toEqual([]);
  });

  it('clears the in-memory preferences when the user logs out', async () => {
    const authStore = useAuthStore();
    authStore.user = user('u-a');
    const store = useDashboardStore();
    store.togglePin('c-a1');
    await nextTick();
    expect(store.pinnedIds).toEqual(['c-a1']);

    authStore.user = null;
    await nextTick();
    expect(store.pinnedIds).toEqual([]);
    // The account's own key still holds its choices.
    expect((stored('u-a') as { pinnedIds?: unknown }).pinnedIds).toEqual(['c-a1']);
  });

  it('stores only preference metadata, never account tokens or calendar contents', () => {
    const authStore = useAuthStore();
    authStore.user = user('u-secrets');
    const store = useDashboardStore();
    store.togglePin('c-17');
    store.setViewMode('list');
    store.setSortMode('custom');

    const saved = stored('u-secrets') as unknown as Record<string, unknown>;
    expect(Object.keys(saved).sort()).toEqual([
      'customOrder',
      'pinnedIds',
      'sortDirection',
      'sortMode',
      'viewMode',
    ]);
    // Only flat scalar preference flags and id arrays — nothing nested, no account or
    // calendar objects, no tokens.
    for (const value of Object.values(saved)) {
      expect(typeof value === 'string' || Array.isArray(value)).toBe(true);
    }
    const serialized = JSON.stringify(saved);
    expect(serialized).not.toContain('access_token');
    expect(serialized).not.toContain('refresh_token');
    expect(serialized).not.toContain('"name"');
  });
});
