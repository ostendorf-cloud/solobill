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

import { apiClient } from './api'
import AppNav from './components/AppNav.vue'
import { clearSession, loadSession, me, status } from './session'
import { gobdBannerVisible } from './status'

const BARE_ROUTES = ['login', 'accept-invite']

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const loggingOut = ref(false)
const logoutError = ref('')

const bare = computed(() => typeof route.name === 'string' && BARE_ROUTES.includes(route.name))
const showBanner = computed(() => me.value !== null && gobdBannerVisible(status.value))

// Entering a bare route ends the session in the shell, whoever ended it: the
// shell's own button, a view's own sign out or a 401. Stepping from there to
// a shell route loads the session again, so a second user never sees the
// first one's name, links or banner.
watch(
  () => route.name,
  (name) => {
    if (name === undefined) {
      return
    }
    if (bare.value) {
      clearSession()
    } else if (me.value === null) {
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
    clearSession()
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
