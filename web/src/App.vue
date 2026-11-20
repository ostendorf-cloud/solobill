<script setup lang="ts">
// App is the shell: skip link, and for a signed in route the header with
// the navigation, the language and theme switch and the sign out button,
// plus the GoBD banner. The login and invite routes render bare. The
// session comes from GET /api/v1/me, the banner from GET /api/v1/status
// (gobdBannerVisible, src/status.ts): a failed status request means unknown
// and shows no banner.
import {
  GovueButton,
  GovueHeader,
  GovueLanguageSwitch,
  GovueSkipLink,
  GovueToastHost,
} from '@ostendorf-cloud/govue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import { apiClient, csrfToken } from './api'
import AppNav from './components/AppNav.vue'
import { gobdBannerVisible, type StatusResponse } from './status'

interface MeResponse {
  email: string
  name: string
  role: string
  csrf_token: string
}

const BARE_ROUTES = ['login', 'accept-invite']

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const me = ref<MeResponse | null>(null)
const status = ref<StatusResponse | null>(null)
const loggingOut = ref(false)
const logoutError = ref('')

const bare = computed(() => typeof route.name === 'string' && BARE_ROUTES.includes(route.name))
const showBanner = computed(() => me.value !== null && gobdBannerVisible(status.value))

async function loadSession(): Promise<void> {
  try {
    const response = await apiClient.get<MeResponse>('/me')
    csrfToken.value = response.csrf_token
    me.value = response
  } catch {
    // Not signed in. The views send the visitor to the login themselves.
    me.value = null
    status.value = null
    return
  }
  try {
    status.value = await apiClient.get<StatusResponse>('/status')
  } catch {
    // 501 before the status endpoint exists, 401: unknown, no banner.
    status.value = null
  }
}

watch(
  () => [route.name, bare.value] as const,
  () => {
    if (!bare.value && route.name !== undefined && me.value === null) {
      void loadSession()
    }
  },
  { immediate: true },
)

function rememberLocale(code: string): void {
  try {
    localStorage.setItem('solobill-locale', code)
  } catch {
    // Blocked storage: the choice only lasts for this visit.
  }
}

async function logout(): Promise<void> {
  loggingOut.value = true
  logoutError.value = ''
  try {
    await apiClient.post('/auth/logout')
    csrfToken.value = undefined
    me.value = null
    status.value = null
    await router.push({ name: 'login' })
  } catch {
    logoutError.value = t('common.logoutError')
  } finally {
    loggingOut.value = false
  }
}
</script>

<template>
  <GovueSkipLink>{{ t('common.skipToContent') }}</GovueSkipLink>

  <GovueHeader v-if="!bare" theme-switch>
    <template #brand>
      <RouterLink to="/" class="text-text">{{ t('common.appName') }}</RouterLink>
    </template>

    <AppNav v-if="me" :role="me.role" />

    <template #actions>
      <GovueLanguageSwitch @change="rememberLocale" />
      <template v-if="me">
        <span class="hidden text-sm text-text-muted lg:inline">
          {{ t('common.signedInAs', { name: me.name || me.email }) }}
        </span>
        <GovueButton
          variant="secondary"
          :loading="loggingOut"
          :loading-label="t('common.loggingOut')"
          @click="logout"
        >
          {{ t('common.logout') }}
        </GovueButton>
      </template>
    </template>
  </GovueHeader>

  <p
    v-if="showBanner"
    role="status"
    data-testid="gobd-banner"
    class="border-b border-border bg-warning-subtle px-4 py-3 text-base text-warning-text sm:px-6"
  >
    {{ t('common.gobdBanner') }}
  </p>
  <p v-if="logoutError" role="alert" class="px-4 py-2 text-danger-text sm:px-6">
    {{ logoutError }}
  </p>

  <RouterView />

  <GovueToastHost />
</template>
