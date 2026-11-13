<script setup lang="ts">
// LoginView proves the local login half of the token contract: on success
// the backend starts a session cookie (see
// internal/httpapi/auth_handlers.go's handleLogin) and this view hands off
// to HomeView, whose own mount check is what actually exercises the
// session GET /api/v1/me needs.
import {
  GovueButton,
  GovueCard,
  GovueField,
  GovueInput,
  GovuePage,
  useForm,
} from '@ostendorf-cloud/govue'
import type { FieldErrors } from '@ostendorf-cloud/govue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { apiClient } from '../api'

const { t } = useI18n()
const router = useRouter()

interface LoginFields {
  tenantSlug: string
  email: string
  password: string
  totpCode: string
}

// The OIDC login route is mounted by internal/httpapi only when
// config.Config.OIDCEnabled is true (see httpapi.go's route registration);
// nothing today exposes that flag to this SPA. The link is shown
// unconditionally rather than guessing at a flag that does not exist: with
// OIDC disabled, the backend's catch-all SPA route answers this same path
// with the shell's own index.html (see internal/httpapi/spa.go), so a
// visitor clicking it with OIDC off lands back on this app instead of at a
// dead JSON 404.
const oidcLoginHref = '/auth/oidc/login'

const form = useForm<LoginFields>({
  initial: { tenantSlug: '', email: '', password: '', totpCode: '' },
  validate: (values) => {
    const errors: FieldErrors<LoginFields> = {}
    if (values.tenantSlug.trim() === '') {
      errors.tenantSlug = t('login.validation.tenantSlug')
    }
    if (values.email.trim() === '') {
      errors.email = t('login.validation.email')
    }
    if (values.password === '') {
      errors.password = t('login.validation.password')
    }
    if (values.totpCode.trim() === '') {
      errors.totpCode = t('login.validation.totpCode')
    }
    return errors
  },
  submit: async (values) => {
    await apiClient.post('/auth/login', {
      tenant_slug: values.tenantSlug,
      email: values.email,
      password: values.password,
      totp_code: values.totpCode,
    })
    await router.push({ name: 'home' })
  },
})
</script>

<template>
  <GovuePage :title="t('login.title')" :lead="t('login.lead')">
    <GovueCard>
      <!-- novalidate: the required attribute below stays for accessibility
           and the field's own styling, but validation feedback is
           useForm's own i18n'd messages, never the browser's native,
           unlocalized constraint validation tooltip. -->
      <form class="flex flex-col gap-4" novalidate @submit.prevent="form.handleSubmit">
        <GovueField :label="t('login.tenantSlug')" :error="form.errorOf('tenantSlug')" required>
          <GovueInput
            v-model="form.values.tenantSlug"
            autocomplete="organization"
            @blur="form.blur('tenantSlug')"
          />
        </GovueField>

        <GovueField :label="t('login.email')" :error="form.errorOf('email')" required>
          <GovueInput
            v-model="form.values.email"
            type="email"
            autocomplete="username"
            @blur="form.blur('email')"
          />
        </GovueField>

        <GovueField :label="t('login.password')" :error="form.errorOf('password')" required>
          <GovueInput
            v-model="form.values.password"
            type="password"
            autocomplete="current-password"
            @blur="form.blur('password')"
          />
        </GovueField>

        <GovueField :label="t('login.totpCode')" :error="form.errorOf('totpCode')" required>
          <GovueInput
            v-model="form.values.totpCode"
            inputmode="numeric"
            autocomplete="one-time-code"
            @blur="form.blur('totpCode')"
          />
        </GovueField>

        <p v-if="form.problem.value" role="alert" class="text-sm text-danger-text">
          {{ form.problem.value }}
        </p>

        <GovueButton
          type="submit"
          variant="primary"
          block
          :loading="form.submitting.value"
          :loading-label="t('login.submitting')"
        >
          {{ t('login.submit') }}
        </GovueButton>
      </form>
    </GovueCard>

    <p class="text-sm">
      <a :href="oidcLoginHref" class="text-accent-text underline">{{ t('login.oidcLink') }}</a>
    </p>
  </GovuePage>
</template>
