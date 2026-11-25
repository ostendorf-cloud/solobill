<script setup lang="ts">
// AssetsView lists the asset register (GET /api/v1/assets): acquisition date,
// cost, method, current book value and state as the server returns them. The
// browser computes no amount and no edit rule. The owner creates an asset from
// an expense with the asset flag; every role reads.
import {
  GovueBadge,
  GovueButton,
  GovueEmptyState,
  GovueErrorState,
  GovuePage,
  GovueSkeleton,
  isApiError,
} from '@ostendorf-cloud/govue'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { apiClient } from '../api'
import { bookValueNote, type Asset } from '../assets'
import AssetFormDialog from '../components/AssetFormDialog.vue'
import UnverifiedNote from '../components/UnverifiedNote.vue'
import { formatCents } from '../money'
import { clearSession, me } from '../session'

const { t, locale } = useI18n()
const router = useRouter()

const assets = ref<Asset[]>([])
const loading = ref(true)
const loadFailed = ref(false)
const loadRequestId = ref('')
const formOpen = ref(false)

const isOwner = computed(() => me.value?.role === 'owner')
const takenExpenseIds = computed(() =>
  assets.value.flatMap((a) => (a.expense_id === null ? [] : [a.expense_id])),
)

// A load that is still running when the view goes away is cancelled, and its
// answer, a 401 included, changes nothing afterwards.
const abort = new AbortController()
onBeforeUnmount(() => abort.abort())

// Each load takes a number. An answer that is not the newest request's is
// dropped.
let latestRequest = 0

async function load(showSkeleton = true): Promise<void> {
  const request = ++latestRequest
  if (showSkeleton) {
    loading.value = true
  }
  loadFailed.value = false
  try {
    const answer = await apiClient.get<{ assets: Asset[] }>('/assets', { signal: abort.signal })
    if (abort.signal.aborted || request !== latestRequest) {
      return
    }
    assets.value = answer.assets
  } catch (error) {
    if (abort.signal.aborted || request !== latestRequest) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      clearSession()
      await router.push({ name: 'login' })
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

onMounted(() => load())

function openAsset(a: Asset): void {
  void router.push({ name: 'asset-detail', params: { id: a.id } })
}
</script>

<template>
  <GovuePage width="shell" :title="t('assets.title')" :lead="t('assets.lead')">
    <UnverifiedNote />

    <div v-if="isOwner">
      <GovueButton variant="primary" @click="formOpen = true">{{ t('assets.create') }}</GovueButton>
    </div>

    <div v-if="loading" role="status">
      <span class="sr-only">{{ t('assets.loading') }}</span>
      <GovueSkeleton :lines="4" />
    </div>

    <GovueErrorState
      v-else-if="loadFailed"
      :title="t('assets.loadErrorTitle')"
      :message="t('assets.loadError')"
      :request-id="loadRequestId || undefined"
      @retry="load()"
    />

    <GovueEmptyState
      v-else-if="assets.length === 0"
      :title="t('assets.emptyTitle')"
      :description="t('assets.emptyDescription')"
    />

    <div v-else class="overflow-x-auto rounded-lg border border-border bg-surface">
      <table class="w-full text-left text-base">
        <caption class="sr-only">
          {{
            t('assets.tableCaption')
          }}
        </caption>
        <thead class="border-b border-border bg-bg-subtle text-sm text-text-muted">
          <tr>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('assets.columns.name') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('assets.columns.acquiredOn') }}</th>
            <th scope="col" class="px-3 py-2 text-right font-medium">
              {{ t('assets.columns.cost') }}
            </th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('assets.columns.method') }}</th>
            <th scope="col" class="px-3 py-2 text-right font-medium">
              {{ t('assets.columns.bookValue') }}
            </th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('assets.columns.state') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('assets.columns.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="a in assets" :key="a.id" class="border-b border-border last:border-b-0">
            <th scope="row" class="px-3 py-2 font-medium">{{ a.name }}</th>
            <td class="px-3 py-2">{{ a.acquired_on }}</td>
            <td class="px-3 py-2 text-right">{{ formatCents(a.cost_basis_cents, locale) }}</td>
            <td class="px-3 py-2">{{ t(`assets.method.${a.method}`) }}</td>
            <td class="px-3 py-2 text-right">
              <div v-if="a.book_value_cents !== null">
                {{ formatCents(a.book_value_cents, locale) }}
              </div>
              <div class="text-sm text-text-muted">{{ bookValueNote(a, t) }}</div>
            </td>
            <td class="px-3 py-2">
              <div class="flex flex-wrap gap-1">
                <GovueBadge :kind="a.disposed_on ? 'neutral' : 'success'">
                  {{ a.disposed_on ? t('assets.state.disposed') : t('assets.state.active') }}
                </GovueBadge>
                <GovueBadge v-if="a.editable" kind="neutral">
                  {{ t('assets.state.editable') }}
                </GovueBadge>
              </div>
            </td>
            <td class="px-3 py-2">
              <GovueButton
                variant="quiet"
                :aria-label="t('assets.openFor', { name: a.name })"
                @click="openAsset(a)"
              >
                {{ t('assets.open') }}
              </GovueButton>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <AssetFormDialog
      v-model:open="formOpen"
      :asset="null"
      :taken-expense-ids="takenExpenseIds"
      @saved="load(false)"
    />
  </GovuePage>
</template>
