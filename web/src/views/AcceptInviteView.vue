<script setup lang="ts">
// AcceptInviteView proves the invite flow's UI side: it reads the token an
// invite link carries in its query string, requests a fresh TOTP secret
// from GET /api/v1/invites/totp-secret (internal/httpapi's
// handleInviteTOTPSecret, which needs no authentication, see its own doc
// comment), and submits POST /api/v1/invites/accept with that secret plus
// the code the invitee's authenticator app now computes from it.
import {
  GovueButton,
  GovueCard,
  GovueField,
  GovueInput,
  GovuePage,
  useForm,
} from '@ostendorf-cloud/govue'
import type { FieldErrors } from '@ostendorf-cloud/govue'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { apiClient } from '../api'

interface TotpSecretResponse {
  secret: string
  provisioning_uri: string
}

interface AcceptFields {
  tenantSlug: string
  token: string
  password: string
  totpCode: string
}

function queryString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const totpSecret = ref('')
const provisioningUri = ref('')
const totpSecretLoading = ref(true)
const totpSecretError = ref('')

onMounted(async () => {
  try {
    const email = queryString(route.query.email) || undefined
    const response = await apiClient.get<TotpSecretResponse>('/invites/totp-secret', {
      query: { email },
    })
    totpSecret.value = response.secret
    provisioningUri.value = response.provisioning_uri
  } catch {
    totpSecretError.value = t('acceptInvite.totpSecretError')
  } finally {
    totpSecretLoading.value = false
  }
})

const form = useForm<AcceptFields>({
  initial: {
    tenantSlug: queryString(route.query.tenant),
    token: queryString(route.query.token),
    password: '',
    totpCode: '',
  },
  validate: (values) => {
    const errors: FieldErrors<AcceptFields> = {}
    if (values.tenantSlug.trim() === '') {
      errors.tenantSlug = t('acceptInvite.validation.tenantSlug')
    }
    if (values.token.trim() === '') {
      errors.token = t('acceptInvite.validation.token')
    }
    if (values.password === '') {
      errors.password = t('acceptInvite.validation.password')
    }
    if (values.totpCode.trim() === '') {
      errors.totpCode = t('acceptInvite.validation.totpCode')
    }
    return errors
  },
  submit: async (values) => {
    await apiClient.post('/invites/accept', {
      tenant_slug: values.tenantSlug,
      token: values.token,
      password: values.password,
      totp_secret: totpSecret.value,
      totp_code: values.totpCode,
    })
    await router.push({ name: 'home' })
  },
})
</script>

<template>
  <GovuePage :title="t('acceptInvite.title')" :lead="t('acceptInvite.lead')">
    <GovueCard>
      <p v-if="totpSecretLoading">{{ t('acceptInvite.totpSecretLoading') }}</p>
      <p v-else-if="totpSecretError" role="alert" class="text-danger-text">
        {{ totpSecretError }}
      </p>
      <dl v-else class="flex flex-col gap-2 text-sm">
        <div>
          <dt class="font-medium text-text-muted">{{ t('acceptInvite.totpSecret') }}</dt>
          <dd class="font-mono break-all">{{ totpSecret }}</dd>
        </div>
        <div>
          <dt class="font-medium text-text-muted">{{ t('acceptInvite.provisioningUri') }}</dt>
          <dd class="font-mono break-all">{{ provisioningUri }}</dd>
        </div>
      </dl>
    </GovueCard>

    <GovueCard>
      <!-- novalidate: see LoginView's form for why. -->
      <form class="flex flex-col gap-4" novalidate @submit.prevent="form.handleSubmit">
        <GovueField
          :label="t('acceptInvite.tenantSlug')"
          :error="form.errorOf('tenantSlug')"
          required
        >
          <GovueInput v-model="form.values.tenantSlug" @blur="form.blur('tenantSlug')" />
        </GovueField>

        <GovueField :label="t('acceptInvite.token')" :error="form.errorOf('token')" required>
          <GovueInput v-model="form.values.token" @blur="form.blur('token')" />
        </GovueField>

        <GovueField :label="t('acceptInvite.password')" :error="form.errorOf('password')" required>
          <GovueInput
            v-model="form.values.password"
            type="password"
            autocomplete="new-password"
            @blur="form.blur('password')"
          />
        </GovueField>

        <GovueField :label="t('acceptInvite.totpCode')" :error="form.errorOf('totpCode')" required>
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
          :loading-label="t('acceptInvite.submitting')"
        >
          {{ t('acceptInvite.submit') }}
        </GovueButton>
      </form>
    </GovueCard>
  </GovuePage>
</template>
