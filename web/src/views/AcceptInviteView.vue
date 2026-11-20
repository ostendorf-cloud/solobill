<script setup lang="ts">
// AcceptInviteView is the invite flow on govue's TOTP enrolment component.
// It reads the tenant slug and token an invite link carries in its query
// string and requests a fresh, server-issued TOTP secret from
// POST /api/v1/invites/totp-secret (internal/httpapi's
// handleInviteTOTPSecret, which persists the secret on the invite, names
// the invite's email and sets the HttpOnly invite cookie). GovueTotpEnrolment
// then shows the secret and posts govue's {login, password, code} to
// POST /api/v1/invites/confirm, which takes the tenant and the token from
// that cookie, sets the password and confirms the code in one step, and
// starts the session. The secret itself never travels back from the client.
import { GovueCard, GovuePage, GovueTotpEnrolment } from '@ostendorf-cloud/govue'
import type { Enrolment } from '@ostendorf-cloud/govue'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { apiClient } from '../api'

interface TotpSecretResponse {
  secret: string
  provisioning_uri: string
  email: string
}

function queryString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const enrolment = ref<Enrolment | undefined>(undefined)
const login = ref('')
const loading = ref(true)
const loadError = ref('')

onMounted(async () => {
  const tenantSlug = queryString(route.query.tenant)
  const token = queryString(route.query.token)

  if (tenantSlug === '' || token === '') {
    loadError.value = t('acceptInvite.totpSecretError')
    loading.value = false
    return
  }

  try {
    const response = await apiClient.post<TotpSecretResponse>('/invites/totp-secret', {
      tenant_slug: tenantSlug,
      token,
    })
    enrolment.value = { secret: response.secret, uri: response.provisioning_uri }
    login.value = response.email
  } catch {
    loadError.value = t('acceptInvite.totpSecretError')
  } finally {
    loading.value = false
  }
})

async function confirmed(): Promise<void> {
  // The secret is shown once and has no reason to stay in memory.
  enrolment.value = undefined
  await router.push({ name: 'home' })
}
</script>

<template>
  <GovuePage :title="t('acceptInvite.title')" :lead="t('acceptInvite.lead')">
    <GovueCard>
      <p v-if="loading">{{ t('acceptInvite.totpSecretLoading') }}</p>
      <p v-else-if="loadError" role="alert" class="text-danger-text">{{ loadError }}</p>
      <GovueTotpEnrolment
        v-else-if="enrolment"
        :client="apiClient"
        :login="login"
        :enrolment="enrolment"
        path="/invites/confirm"
        @confirmed="confirmed"
      />
    </GovueCard>
  </GovuePage>
</template>
