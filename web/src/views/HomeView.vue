<script setup lang="ts">
// HomeView is the authenticated shell's proof that the session and the
// CSRF token contract both work end to end: it reads GET /api/v1/me,
// stores the csrf_token it answers with (see internal/httpapi's
// meResponse), and then spends that token on a real mutating request, the
// logout button's POST /api/v1/auth/logout, which the server's CSRF
// protector middleware would reject without it.
import { isApiError, GovueButton, GovueCard, GovuePage } from '@ostendorf-cloud/govue'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { apiClient, csrfToken } from '../api'

interface MeResponse {
  email: string
  name: string
  role: string
  tenant_id: string
  csrf_token: string
}

const { t } = useI18n()
const router = useRouter()

const profile = ref<MeResponse | undefined>(undefined)
const loading = ref(true)
const loadError = ref('')
const loggingOut = ref(false)
const logoutError = ref('')

onMounted(async () => {
  try {
    const me = await apiClient.get<MeResponse>('/me')
    profile.value = me
    csrfToken.value = me.csrf_token
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      await router.push({ name: 'login' })
      return
    }
    loadError.value = t('home.loadError')
  } finally {
    loading.value = false
  }
})

async function logout(): Promise<void> {
  logoutError.value = ''
  loggingOut.value = true
  try {
    await apiClient.post('/auth/logout')
    csrfToken.value = undefined
    await router.push({ name: 'login' })
  } catch {
    logoutError.value = t('home.logoutError')
  } finally {
    loggingOut.value = false
  }
}
</script>

<template>
  <GovuePage :title="t('home.title')" :lead="t('home.lead')">
    <p v-if="loading">{{ t('home.loading') }}</p>

    <p v-else-if="loadError" role="alert" class="text-danger-text">{{ loadError }}</p>

    <GovueCard v-else-if="profile">
      <dl class="flex flex-col gap-2">
        <div>
          <dt class="text-sm font-medium text-text-muted">{{ t('home.email') }}</dt>
          <dd>{{ profile.email }}</dd>
        </div>
        <div>
          <dt class="text-sm font-medium text-text-muted">{{ t('home.name') }}</dt>
          <dd>{{ profile.name }}</dd>
        </div>
        <div>
          <dt class="text-sm font-medium text-text-muted">{{ t('home.role') }}</dt>
          <dd>{{ profile.role }}</dd>
        </div>
        <div>
          <dt class="text-sm font-medium text-text-muted">{{ t('home.tenantId') }}</dt>
          <dd>{{ profile.tenant_id }}</dd>
        </div>
      </dl>

      <template #footer>
        <GovueButton
          variant="secondary"
          :loading="loggingOut"
          :loading-label="t('home.loggingOut')"
          @click="logout"
        >
          {{ t('home.logout') }}
        </GovueButton>
      </template>
    </GovueCard>

    <p v-if="logoutError" role="alert" class="text-danger-text">{{ logoutError }}</p>
  </GovuePage>
</template>
