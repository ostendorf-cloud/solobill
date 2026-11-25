<script setup lang="ts">
// LoginView is the local login through govue's own components: the tenant
// comes first, because the login routes carry it in their path
// (POST /api/v1/tenants/{tenant}/auth/login and the passkey login under
// /api/v1/tenants/{tenant}/auth/passkeys), then govue's GovueLoginForm
// (login, password, TOTP code in one request) and GovuePasskeyButton. On
// success the backend has started a session cookie, and this view hands
// off to HomeView, whose own mount check exercises GET /api/v1/me.
import {
  GovueCard,
  GovueField,
  GovueInput,
  GovueLoginForm,
  GovuePage,
  GovuePasskeyButton,
} from '@ostendorf-cloud/govue'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { apiClient } from '../api'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

// An invite or a bookmark may carry the tenant as ?tenant=, so the field
// starts filled.
const tenantSlug = ref(typeof route.query.tenant === 'string' ? route.query.tenant : '')
const tenantPath = computed(() => `/tenants/${encodeURIComponent(tenantSlug.value.trim())}/auth`)
const noTenant = computed(() => tenantSlug.value.trim() === '')

// The OIDC login route is mounted by internal/httpapi only when OIDC is
// configured, and answers a JSON 404 otherwise. GET /api/v1/auth/methods
// says whether it is, without a session, so the link shows only then. A
// failed request leaves the link hidden: a missing link is the safe way
// to be wrong, a link to a 404 is not.
const oidcLoginHref = '/api/v1/auth/oidc/login'
const oidcEnabled = ref(false)

interface AuthMethods {
  oidc: boolean
}

onMounted(async () => {
  try {
    const methods = await apiClient.get<AuthMethods>('/auth/methods', {
      skipUnauthenticatedHook: true,
    })
    oidcEnabled.value = methods?.oidc === true
  } catch {
    oidcEnabled.value = false
  }
})

async function loggedIn(): Promise<void> {
  await router.push({ name: 'home' })
}
</script>

<template>
  <GovuePage :title="t('login.title')" :lead="t('login.lead')">
    <GovueCard>
      <div class="flex flex-col gap-4">
        <GovueField
          :label="t('login.tenantSlug')"
          :hint="noTenant ? t('login.validation.tenantSlug') : undefined"
          required
        >
          <GovueInput v-model="tenantSlug" autocomplete="organization" />
        </GovueField>

        <GovueLoginForm
          :client="apiClient"
          :path="`${tenantPath}/login`"
          :disabled="noTenant"
          @success="loggedIn"
        />

        <GovuePasskeyButton
          :client="apiClient"
          mode="login"
          :path="`${tenantPath}/passkeys`"
          :disabled="noTenant"
          @success="loggedIn"
        />
      </div>
    </GovueCard>

    <p v-if="oidcEnabled" class="text-sm">
      <a :href="oidcLoginHref" class="text-accent-text underline">{{ t('login.oidcLink') }}</a>
    </p>
  </GovuePage>
</template>
