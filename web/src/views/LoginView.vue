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
import { computed, ref } from 'vue'
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

// The OIDC login route is mounted by internal/httpapi only when
// config.Config.OIDCEnabled is true (see httpapi.go's route registration);
// nothing today exposes that flag to this SPA. The link is shown
// unconditionally rather than guessing at a flag that does not exist: with
// OIDC disabled, the backend's catch-all SPA route answers this same path
// with the shell's own index.html (see internal/httpapi/spa.go), so a
// visitor clicking it with OIDC off lands back on this app instead of at a
// dead JSON 404.
const oidcLoginHref = '/auth/oidc/login'

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

    <p class="text-sm">
      <a :href="oidcLoginHref" class="text-accent-text underline">{{ t('login.oidcLink') }}</a>
    </p>
  </GovuePage>
</template>
