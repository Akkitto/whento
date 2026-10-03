/*
 * WhenTo - Collaborative event calendar for self-hosted environments
 * Copyright (C) 2025 WhenTo Contributors
 * SPDX-License-Identifier: BSL-1.1
 */

/**
 * Development-only preview of the real dashboard on fabricated data.
 *
 * The dashboard list and the unified feed live behind the API, which makes the views
 * impossible to exercise without a full stack. This entry mounts the *real*
 * `Dashboard.vue` (stores, locale, ordering, focus handling and all) against fixture
 * data by stubbing the two API objects the view fetches on mount, so Playwright can
 * drive a genuine keyboard session against it with nothing running behind it.
 *
 * Served by `vite dev` at /dev/preview-dashboard.html. It is not part of the
 * production build: Vite's default rollup input is index.html only.
 */

import { createApp } from 'vue';
import { createPinia } from 'pinia';
import { createI18n } from 'vue-i18n';
import { createRouter, createWebHashHistory } from 'vue-router';
import en from '../src/locales/en.json';
import fr from '../src/locales/fr.json';
import '../src/style.css';
import DashboardPreviewApp from './DashboardPreviewApp.vue';
import Dashboard from '../src/views/Dashboard.vue';
import { calendarsApi } from '../src/api/calendars';
import { unifiedFeedApi } from '../src/api/unifiedFeed';
import { useAuthStore } from '../src/stores/auth';
import type { CalendarWithParticipants } from '../src/types';

/**
 * Four calendars whose names sort alphabetically, so the custom-order seed and the
 * move paths in the e2e are deterministic.
 */
const FABRICATED_CALENDARS = [
  { id: 'alpha', name: 'Alpha', public_token: 'tok-alpha', participants: [] },
  { id: 'bravo', name: 'Bravo', public_token: 'tok-bravo', participants: [] },
  { id: 'charlie', name: 'Charlie', public_token: 'tok-charlie', participants: [] },
  { id: 'delta', name: 'Delta', public_token: 'tok-delta', participants: [] },
] as unknown as CalendarWithParticipants[];

// Patch the API objects the dashboard fetches on mount so they succeed offline.
calendarsApi.getAll = (async () => FABRICATED_CALENDARS) as typeof calendarsApi.getAll;
unifiedFeedApi.getConfig = (async () => ({ configured: false })) as typeof unifiedFeedApi.getConfig;

const i18n = createI18n({
  legacy: false,
  locale: new URLSearchParams(window.location.search).get('lang') || 'en',
  fallbackLocale: 'en',
  messages: { en, fr },
});
const pinia = createPinia();

const authStore = useAuthStore(pinia);
authStore.user = { id: 'preview-user', display_name: 'Preview' } as never;

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', component: Dashboard },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
});

createApp(DashboardPreviewApp).use(pinia).use(i18n).use(router).mount('#app');
