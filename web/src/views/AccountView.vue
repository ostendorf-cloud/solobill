<script setup lang="ts">
// AccountView is the personal settings of the signed in user: the language
// and the colour mode. Both controls are govue's own, and both remember the
// choice in this browser (govue's language and theme storage keys), so the
// boot script of the frontend server and the next visit read it again. The
// choice is not stored per account on the server. A visitor without a
// session goes to the login, like every other view.
import {
  GovueLanguageSwitch,
  GovuePage,
  GovueThemeSwitch,
  isApiError,
  useLanguage,
  useTheme,
} from '@ostendorf-cloud/govue'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { apiClient } from '../api'
import { localizedHref } from '../router/locale'
import { loadSession, me } from '../session'

const { t } = useI18n()
const router = useRouter()
const { mode } = useTheme()
const { locale, locales } = useLanguage()

const loadError = ref(false)

const languageName = computed(
  () => locales.find((entry) => entry.code === locale.value)?.name ?? locale.value,
)
const modeName = computed(() => t(`account.colourMode.${mode.value}`))

onMounted(async () => {
  if (me.value !== null) {
    return
  }
  try {
    await apiClient.get('/me')
    await loadSession()
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      await router.push({ name: 'login' })
      return
    }
    loadError.value = true
  }
})

// The language switch is a group of links to this page in the other
// language. govue remembers the choice; the router takes the click over.
function goToLanguage(href: string): boolean {
  void router.push(href)
  return true
}
</script>

<template>
  <GovuePage :title="t('account.title')" :lead="t('account.lead')">
    <p v-if="loadError" role="alert" class="mb-6 text-danger-text">
      {{ t('account.loadError') }}
    </p>

    <section aria-labelledby="account-language" class="mb-8">
      <h2 id="account-language" class="mb-2 text-xl font-semibold">
        {{ t('account.language.title') }}
      </h2>
      <p class="mb-3 text-text-muted" data-testid="current-language">
        {{ t('account.language.current', { name: languageName }) }}
      </p>
      <GovueLanguageSwitch
        :label="t('account.language.label')"
        :link-for="(code) => localizedHref(router, code)"
        :navigate="goToLanguage"
      />
    </section>

    <section aria-labelledby="account-colour-mode">
      <h2 id="account-colour-mode" class="mb-2 text-xl font-semibold">
        {{ t('account.colourMode.title') }}
      </h2>
      <p class="mb-3 text-text-muted" data-testid="current-mode">
        {{ t('account.colourMode.current', { mode: modeName }) }}
      </p>
      <GovueThemeSwitch
        :label="t('account.colourMode.label')"
        :system-label="t('account.colourMode.system')"
        :light-label="t('account.colourMode.light')"
        :dark-label="t('account.colourMode.dark')"
      />
    </section>
  </GovuePage>
</template>
