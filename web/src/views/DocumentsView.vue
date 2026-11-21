<script setup lang="ts">
// DocumentsView lists offers, invoices and corrections (GET
// /api/v1/documents) with the filters of the API. An issued document shows
// its stored number and its chain status; a draft shows no number. A
// superseded document is marked in words, not by colour alone.
import {
  GovueBadge,
  GovueButton,
  GovueEmptyState,
  GovueErrorState,
  GovueField,
  GovuePage,
  GovueSelect,
  GovueSkeleton,
  isApiError,
} from '@ostendorf-cloud/govue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { apiClient } from '../api'
import { berlinToday, listDocuments, type Doc } from '../documents'
import { formatCents } from '../money'
import { clearSession } from '../session'

interface CustomerName {
  id: string
  name: string
}

const { t, locale } = useI18n()
const router = useRouter()

const thisYear = new Date().getFullYear()
// A select cannot hold an empty value, so "all" has a value of its own.
const ALL = 'all'
const type = ref(ALL)
const state = ref(ALL)
const year = ref(ALL)

const documents = ref<Doc[]>([])
const customers = ref<Map<string, string>>(new Map())
// Customers GET /customers does not list (deleted ones), read one by one.
const deletedNames = ref<Map<string, string>>(new Map())
const loading = ref(true)
const loadFailed = ref(false)
const loadRequestId = ref('')

const typeOptions = computed(() => [
  { value: ALL, label: t('documents.filters.allTypes') },
  { value: 'invoice', label: t('documents.type.invoice') },
  { value: 'correction', label: t('documents.type.correction') },
  { value: 'offer', label: t('documents.type.offer') },
])
const stateOptions = computed(() => [
  { value: ALL, label: t('documents.filters.allStates') },
  { value: 'draft', label: t('documents.state.draft') },
  { value: 'issued', label: t('documents.state.issued') },
])
const yearOptions = computed(() => [
  { value: ALL, label: t('documents.filters.allYears') },
  ...Array.from({ length: 8 }, (_, i) => String(thisYear + 1 - i)).map((y) => ({
    value: y,
    label: y,
  })),
])

// The view going away ends every read it has started, and an answer or a 401
// that still arrives changes nothing and sends nobody anywhere.
const unmounted = new AbortController()
// The single customer reads of the newest list; a newer list cancels them.
let lookups = new AbortController()
const failedNames = new Set<string>()
onBeforeUnmount(() => {
  unmounted.abort()
  lookups.abort()
})

async function toLogin(): Promise<void> {
  clearSession()
  await router.push({ name: 'login' })
}

// Each load takes a number. An answer that is not the newest request's is
// dropped, so a slow earlier filter cannot overwrite a newer list.
let latestRequest = 0

async function load(showSkeleton = true): Promise<void> {
  const request = ++latestRequest
  lookups.abort()
  lookups = new AbortController()
  const lookupSignal = lookups.signal
  if (showSkeleton) {
    loading.value = true
  }
  loadFailed.value = false
  const stale = (): boolean => request !== latestRequest || unmounted.signal.aborted
  try {
    const [list, names] = await Promise.allSettled([
      listDocuments(
        {
          type: type.value === ALL ? undefined : type.value,
          state: state.value === ALL ? undefined : state.value,
          year: year.value === ALL ? undefined : year.value,
        },
        { signal: unmounted.signal },
      ),
      apiClient.get<{ customers: CustomerName[] }>('/customers', { signal: unmounted.signal }),
    ])
    if (stale()) {
      return
    }
    for (const result of [list, names]) {
      if (
        result.status === 'rejected' &&
        isApiError(result.reason) &&
        result.reason.isUnauthenticated
      ) {
        await toLogin()
        return
      }
    }
    if (list.status === 'rejected') {
      loadRequestId.value = isApiError(list.reason) ? list.reason.requestId : ''
      loadFailed.value = true
      return
    }
    documents.value = list.value
    // Without the names the rows show a dash instead of failing the list.
    if (names.status === 'fulfilled') {
      customers.value = new Map(names.value.customers.map((c) => [c.id, c.name] as const))
    }
    // When the list of active customers itself failed, every customer would
    // look missing: the rows keep their dashes and nothing is read per id.
    if (names.status === 'fulfilled') {
      void loadDeletedNames(stale, lookupSignal)
    }
  } finally {
    if (!stale()) {
      loading.value = false
    }
  }
}

/** MAX_NAME_LOOKUPS bounds the single reads of customers that the list of
 *  active customers does not hold. */
const MAX_NAME_LOOKUPS = 20

/** loadDeletedNames reads the customers of the listed documents that
 *  GET /customers does not answer for, which are the deleted ones:
 *  GET /customers/{id} still does. Each id is read once, a failure leaves a
 *  dash, and a newer list or an unmount drops the answers. */
async function loadDeletedNames(stale: () => boolean, signal: AbortSignal): Promise<void> {
  const ids = [...new Set(documents.value.map((d) => d.customer_id))]
    .filter((id) => !customers.value.has(id) && !deletedNames.value.has(id) && !failedNames.has(id))
    .slice(0, MAX_NAME_LOOKUPS)
  if (ids.length === 0) {
    return
  }
  const answers = await Promise.allSettled(
    ids.map((id) =>
      apiClient.get<CustomerName>(`/customers/${encodeURIComponent(id)}`, {
        signal,
      }),
    ),
  )
  if (stale()) {
    return
  }
  const found = new Map(deletedNames.value)
  for (const [i, answer] of answers.entries()) {
    if (answer.status === 'rejected') {
      if (isApiError(answer.reason) && answer.reason.isUnauthenticated) {
        await toLogin()
        return
      }
      // A failed id is not read again for as long as the view lives.
      failedNames.add(ids[i] ?? '')
    } else if (typeof answer.value.name === 'string') {
      found.set(ids[i] ?? '', answer.value.name)
    }
  }
  deletedNames.value = found
}

onMounted(() => load())

// A filter change reloads without the skeleton, so the filters keep the focus.
watch([type, state, year], () => load(false))

function customerName(d: Doc): string {
  return customers.value.get(d.customer_id) ?? deletedNames.value.get(d.customer_id) ?? '–'
}

function rowName(d: Doc): string {
  return d.number ?? t(`documents.draftName.${d.type}`)
}

function issuedOn(d: Doc): string {
  return d.issued_at ? berlinToday(new Date(d.issued_at)) : ''
}

function open(d: Doc): void {
  void router.push({ name: 'document-detail', params: { id: d.id } })
}

function create(kind: 'invoice' | 'offer'): void {
  void router.push({ name: 'document-edit', query: { type: kind } })
}
</script>

<template>
  <GovuePage width="shell" :title="t('documents.list.title')" :lead="t('documents.list.lead')">
    <div class="flex flex-wrap gap-3">
      <GovueButton variant="primary" @click="create('invoice')">
        {{ t('documents.list.newInvoice') }}
      </GovueButton>
      <GovueButton variant="secondary" @click="create('offer')">
        {{ t('documents.list.newOffer') }}
      </GovueButton>
    </div>

    <form
      class="grid gap-4 sm:grid-cols-3"
      role="search"
      :aria-label="t('documents.filters.label')"
      @submit.prevent
    >
      <GovueField :label="t('documents.filters.type')">
        <GovueSelect v-model="type" :options="typeOptions" />
      </GovueField>
      <GovueField :label="t('documents.filters.state')">
        <GovueSelect v-model="state" :options="stateOptions" />
      </GovueField>
      <GovueField :label="t('documents.filters.year')">
        <GovueSelect v-model="year" :options="yearOptions" />
      </GovueField>
    </form>

    <div v-if="loading" role="status">
      <span class="sr-only">{{ t('documents.list.loading') }}</span>
      <GovueSkeleton :lines="4" />
    </div>

    <GovueErrorState
      v-else-if="loadFailed"
      :title="t('documents.list.loadErrorTitle')"
      :message="t('documents.list.loadError')"
      :request-id="loadRequestId || undefined"
      @retry="load()"
    />

    <GovueEmptyState
      v-else-if="documents.length === 0"
      :title="t('documents.list.emptyTitle')"
      :description="t('documents.list.emptyDescription')"
    />

    <div v-else class="overflow-x-auto rounded-lg border border-border bg-surface">
      <table class="w-full text-left text-base">
        <caption class="sr-only">
          {{
            t('documents.list.tableCaption')
          }}
        </caption>
        <thead class="border-b border-border bg-bg-subtle text-sm text-text-muted">
          <tr>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('documents.columns.number') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('documents.columns.type') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">
              {{ t('documents.columns.customer') }}
            </th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('documents.columns.state') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('documents.columns.chain') }}</th>
            <th scope="col" class="px-3 py-2 text-right font-medium">
              {{ t('documents.columns.total') }}
            </th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('documents.columns.issuedOn') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('documents.columns.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="d in documents"
            :key="d.id"
            class="border-b border-border last:border-b-0"
            :class="d.chain_status === 'superseded' ? 'text-text-muted' : ''"
            :data-chain="d.chain_status ?? undefined"
          >
            <th scope="row" class="px-3 py-2 font-medium">
              <template v-if="d.number">{{ d.number }}</template>
              <GovueBadge v-else kind="neutral">{{ t('documents.draftMark') }}</GovueBadge>
            </th>
            <td class="px-3 py-2">{{ t(`documents.type.${d.type}`) }}</td>
            <td class="px-3 py-2">{{ customerName(d) }}</td>
            <td class="px-3 py-2">
              <GovueBadge :kind="d.state === 'issued' ? 'success' : 'neutral'">
                {{ t(`documents.state.${d.state}`) }}
              </GovueBadge>
            </td>
            <td class="px-3 py-2">
              <GovueBadge
                v-if="d.chain_status"
                :kind="d.chain_status === 'effective' ? 'info' : 'warning'"
              >
                {{ t(`documents.chain.${d.chain_status}`) }}
              </GovueBadge>
            </td>
            <td class="px-3 py-2 text-right">
              <template v-if="d.totals">{{
                formatCents(d.totals.grand_total_cents, locale)
              }}</template>
              <template v-else>–</template>
            </td>
            <td class="px-3 py-2">{{ issuedOn(d) }}</td>
            <td class="px-3 py-2">
              <GovueButton
                variant="quiet"
                :aria-label="
                  t('documents.list.openFor', { name: rowName(d), customer: customerName(d) })
                "
                @click="open(d)"
              >
                {{ t('documents.list.open') }}
              </GovueButton>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </GovuePage>
</template>
