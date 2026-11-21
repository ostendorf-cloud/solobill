<script setup lang="ts">
// AssetDetailView shows one asset and its depreciation schedule per year
// (GET /api/v1/assets/{id} and /schedule). The schedule is computed by the
// server and only shown here. The owner edits the asset while its first
// schedule year is open (the button is disabled afterwards, with the reason,
// and a 409 of the server is handled anyway) and records a disposal with a
// date.
import {
  isApiError,
  useToasts,
  GovueBadge,
  GovueButton,
  GovueDialog,
  GovueErrorState,
  GovueField,
  GovueInput,
  GovuePage,
  GovueSkeleton,
} from '@ostendorf-cloud/govue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { apiClient } from '../api'
import { assetFieldErrors, editBlock, germanToday, type Asset, type Schedule } from '../assets'
import AssetFormDialog from '../components/AssetFormDialog.vue'
import UnverifiedNote from '../components/UnverifiedNote.vue'
import type { Category } from '../expenses'
import { formatCents } from '../money'
import { clearSession, loadSession, me, sessionFailed, sessionReady } from '../session'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const { notify } = useToasts()

const asset = ref<Asset | null>(null)
const schedule = ref<Schedule | null>(null)
const categories = ref<Category[]>([])
const loading = ref(true)
const loadFailed = ref(false)
const loadRequestId = ref('')

const editOpen = ref(false)
const disposalOpen = ref(false)
const disposedOn = ref('')
const disposalErrors = ref<Record<string, string>>({})
const disposalError = ref('')
const disposing = ref(false)

const isOwner = computed(() => me.value?.role === 'owner')
// Computed from the loaded asset and the clock when the asset loads, not
// ticking over New Year. That is fine: it is only a hint, and the server's 409
// is the authority.
const block = computed(() => (asset.value ? editBlock(asset.value) : null))
const lineLabels = computed(
  () => new Map(categories.value.map((c) => [c.key, c.label_de] as const)),
)

// Requests still running when the view goes away are cancelled, and their
// answers, a 401 included, change nothing afterwards.
const abort = new AbortController()
onBeforeUnmount(() => abort.abort())

function routeId(): string {
  const id = route.params.id
  return typeof id === 'string' ? id : ''
}

async function toLogin(): Promise<void> {
  if (abort.signal.aborted) {
    return
  }
  clearSession()
  await router.push({ name: 'login' })
}

// Each load takes a number. An answer that is not the newest request's is
// dropped.
let latestRequest = 0

async function load(showSkeleton = true): Promise<void> {
  const request = ++latestRequest
  const id = routeId()
  if (showSkeleton) {
    loading.value = true
  }
  loadFailed.value = false
  try {
    const [loaded, plan] = await Promise.all([
      apiClient.get<Asset>(`/assets/${encodeURIComponent(id)}`, { signal: abort.signal }),
      apiClient.get<Schedule>(`/assets/${encodeURIComponent(id)}/schedule`, {
        signal: abort.signal,
      }),
    ])
    if (abort.signal.aborted || request !== latestRequest) {
      return
    }
    asset.value = loaded
    schedule.value = plan
  } catch (error) {
    if (abort.signal.aborted || request !== latestRequest) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    loadRequestId.value = isApiError(error) ? error.requestId : ''
    loadFailed.value = true
  } finally {
    if (!abort.signal.aborted && request === latestRequest) {
      loading.value = false
    }
  }
}

async function loadCategories(): Promise<void> {
  try {
    const answer = await apiClient.get<{ categories: Category[] }>('/categories', {
      signal: abort.signal,
    })
    if (!abort.signal.aborted) {
      categories.value = answer.categories
    }
  } catch (error) {
    if (!abort.signal.aborted && isApiError(error) && error.isUnauthenticated) {
      await toLogin()
    }
    // Without the names the table shows the keys.
  }
}

onMounted(async () => {
  await Promise.all([load(), loadCategories()])
})

watch(
  () => route.params.id,
  (id, previous) => {
    if (typeof id === 'string' && id !== '' && id !== previous) {
      void load()
    }
  },
)

function openEdit(): void {
  editOpen.value = true
}

function askDisposal(): void {
  disposedOn.value = germanToday()
  disposalErrors.value = {}
  disposalError.value = ''
  disposalOpen.value = true
  if (!sessionReady.value) {
    void loadSession()
  }
}

async function focusFirstInvalid(): Promise<void> {
  await nextTick()
  setTimeout(() => {
    document.querySelector<HTMLElement>('#disposal-form [aria-invalid="true"]')?.focus()
  }, 0)
}

async function confirmDisposal(): Promise<void> {
  if (!asset.value) {
    return
  }
  disposalErrors.value = {}
  disposalError.value = ''
  if (!sessionReady.value) {
    disposalError.value = t('common.errors.session')
    void loadSession()
    return
  }
  disposing.value = true
  try {
    await apiClient.post(
      `/assets/${asset.value.id}/disposal`,
      { disposed_on: disposedOn.value.trim(), proceeds_cents: null },
      { signal: abort.signal },
    )
    if (abort.signal.aborted) {
      return
    }
    disposalOpen.value = false
    notify({ kind: 'success', title: t('assets.disposal.done') })
    await load(false)
  } catch (error) {
    if (abort.signal.aborted) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      disposalOpen.value = false
      await toLogin()
      return
    }
    if (isApiError(error) && error.status === 403) {
      disposalError.value = t('common.errors.forbidden')
    } else if (isApiError(error) && error.status === 409) {
      disposalError.value = t('assets.disposal.conflict')
    } else if (isApiError(error) && error.status === 400) {
      disposalErrors.value = assetFieldErrors(error.details, t, locale.value)
      disposalError.value = t('assets.disposal.error')
      void focusFirstInvalid()
    } else if (isApiError(error) && error.status >= 500) {
      disposalError.value = t('common.errors.server')
    } else if (isApiError(error) && error.isTransport) {
      disposalError.value = t('common.errors.network')
    } else {
      disposalError.value = t('common.errors.generic')
    }
  } finally {
    if (!abort.signal.aborted) {
      disposing.value = false
    }
  }
}
</script>

<template>
  <GovuePage width="shell" :title="asset?.name ?? t('assets.title')">
    <div>
      <GovueButton variant="quiet" @click="router.push({ name: 'assets' })">
        {{ t('assets.back') }}
      </GovueButton>
    </div>

    <div v-if="loading" role="status">
      <span class="sr-only">{{ t('assets.detail.loading') }}</span>
      <GovueSkeleton :lines="4" />
    </div>

    <GovueErrorState
      v-else-if="loadFailed || !asset || !schedule"
      :title="t('assets.loadErrorTitle')"
      :message="t('assets.detail.loadError')"
      :request-id="loadRequestId || undefined"
      @retry="load()"
    />

    <template v-else>
      <UnverifiedNote />

      <section class="flex flex-col gap-3" :aria-label="t('assets.detail.facts')">
        <dl class="grid gap-x-6 gap-y-2 sm:grid-cols-2">
          <div>
            <dt class="text-sm text-text-muted">{{ t('assets.columns.acquiredOn') }}</dt>
            <dd>{{ asset.acquired_on }}</dd>
          </div>
          <div>
            <dt class="text-sm text-text-muted">{{ t('assets.columns.cost') }}</dt>
            <dd>{{ formatCents(asset.cost_basis_cents, locale) }}</dd>
          </div>
          <div>
            <dt class="text-sm text-text-muted">{{ t('assets.columns.method') }}</dt>
            <dd>{{ t(`assets.method.${asset.method}`) }}</dd>
          </div>
          <div>
            <dt class="text-sm text-text-muted">{{ t('assets.detail.group') }}</dt>
            <dd>{{ t(`assets.group.${asset.group}`) }}</dd>
          </div>
          <div v-if="asset.useful_life_months !== null">
            <dt class="text-sm text-text-muted">{{ t('assets.detail.usefulLife') }}</dt>
            <dd>{{ asset.useful_life_months }}</dd>
          </div>
          <div v-if="asset.opening_book_value_cents !== null">
            <dt class="text-sm text-text-muted">
              {{ t('assets.detail.openingBookValue', { year: asset.opening_book_value_year }) }}
            </dt>
            <dd>{{ formatCents(asset.opening_book_value_cents, locale) }}</dd>
          </div>
          <div>
            <dt class="text-sm text-text-muted">{{ t('assets.columns.state') }}</dt>
            <dd>
              <GovueBadge :kind="asset.disposed_on ? 'neutral' : 'success'">
                {{ asset.disposed_on ? t('assets.state.disposed') : t('assets.state.active') }}
              </GovueBadge>
            </dd>
          </div>
          <div v-if="asset.disposed_on">
            <dt class="text-sm text-text-muted">{{ t('assets.detail.disposedOn') }}</dt>
            <dd>{{ asset.disposed_on }}</dd>
          </div>
          <div v-if="asset.disposal_proceeds_cents !== null">
            <dt class="text-sm text-text-muted">{{ t('assets.detail.proceeds') }}</dt>
            <dd>{{ formatCents(asset.disposal_proceeds_cents, locale) }}</dd>
          </div>
        </dl>
        <p v-if="asset.imported" class="text-text-muted">{{ t('assets.detail.imported') }}</p>

        <div v-if="isOwner" class="flex flex-wrap gap-2">
          <GovueButton variant="secondary" :disabled="block !== null" @click="openEdit">
            {{ t('assets.edit') }}
          </GovueButton>
          <GovueButton
            variant="secondary"
            :disabled="asset.disposed_on !== null"
            @click="askDisposal"
          >
            {{ t('assets.disposal.action') }}
          </GovueButton>
        </div>
        <p v-if="isOwner && block" data-testid="edit-blocked" class="text-text-muted">
          {{ t(`assets.editBlocked.${block}`) }}
        </p>
      </section>

      <h2 class="text-xl font-semibold">{{ t('assets.detail.scheduleTitle') }}</h2>
      <p v-if="schedule.years.length === 0" class="text-text-muted">
        {{ t('assets.detail.scheduleEmpty') }}
      </p>
      <div v-else class="overflow-x-auto rounded-lg border border-border bg-surface">
        <table class="w-full text-left text-base">
          <caption class="sr-only">
            {{
              t('assets.detail.scheduleCaption')
            }}
          </caption>
          <thead class="border-b border-border bg-bg-subtle text-sm text-text-muted">
            <tr>
              <th scope="col" class="px-3 py-2 font-medium">{{ t('assets.columns.year') }}</th>
              <th scope="col" class="px-3 py-2 text-right font-medium">
                {{ t('assets.columns.opening') }}
              </th>
              <th scope="col" class="px-3 py-2 text-right font-medium">
                {{ t('assets.columns.depreciation') }}
              </th>
              <th scope="col" class="px-3 py-2 text-right font-medium">
                {{ t('assets.columns.closing') }}
              </th>
              <th scope="col" class="px-3 py-2 font-medium">{{ t('assets.columns.euerLine') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="y in schedule.years"
              :key="y.year"
              class="border-b border-border last:border-b-0"
            >
              <th scope="row" class="px-3 py-2 font-medium">{{ y.year }}</th>
              <td class="px-3 py-2 text-right">
                {{ formatCents(y.opening_book_value_cents, locale) }}
              </td>
              <td class="px-3 py-2 text-right">{{ formatCents(y.depreciation_cents, locale) }}</td>
              <td class="px-3 py-2 text-right">
                {{ formatCents(y.closing_book_value_cents, locale) }}
              </td>
              <td class="px-3 py-2">{{ lineLabels.get(y.euer_key) ?? y.euer_key }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <AssetFormDialog v-model:open="editOpen" :asset="asset" @saved="load(false)" />

    <GovueDialog v-model:open="disposalOpen" :title="t('assets.disposal.title')">
      <form
        id="disposal-form"
        class="flex flex-col gap-4"
        novalidate
        @submit.prevent="confirmDisposal"
      >
        <p>{{ t('assets.disposal.message') }}</p>
        <GovueField
          :label="t('assets.disposal.disposedOn')"
          :hint="t('assets.disposal.disposedOnHint')"
          :error="disposalErrors.disposedOn"
        >
          <GovueInput v-model="disposedOn" autocomplete="off" inputmode="numeric" />
        </GovueField>
        <p v-if="disposalError" role="alert" class="text-danger-text">{{ disposalError }}</p>
        <p v-if="sessionFailed && !sessionReady" role="alert" class="text-danger-text">
          {{ t('common.errors.session') }}
        </p>
      </form>
      <template #actions>
        <GovueButton variant="secondary" :disabled="disposing" @click="disposalOpen = false">
          {{ t('assets.disposal.cancel') }}
        </GovueButton>
        <GovueButton
          type="submit"
          form="disposal-form"
          variant="primary"
          :disabled="!sessionReady"
          :loading="disposing"
          :loading-label="t('assets.disposal.confirm')"
        >
          {{ t('assets.disposal.confirm') }}
        </GovueButton>
      </template>
    </GovueDialog>
  </GovuePage>
</template>
