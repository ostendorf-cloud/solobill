<script setup lang="ts">
// ExpensesView lists the expenses of one year (GET /api/v1/expenses) with
// filters for the state and the category and the totals the API computes.
// The category column shows the German EÜR line name from
// GET /api/v1/categories, the stored value is its key.
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
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { apiClient } from '../api'
import type { Category, Expense } from '../expenses'
import { formatCents } from '../money'
import { clearSession } from '../session'

interface ListAnswer {
  expenses: Expense[]
  total_gross_cents: number
  total_net_cents: number
}

const { t, locale } = useI18n()
const router = useRouter()

const thisYear = new Date().getFullYear()
const year = ref(String(thisYear))
// The select cannot hold an empty value, so "all" has a value of its own.
const ALL = 'all'
const state = ref(ALL)
const categoryKey = ref(ALL)

const expenses = ref<Expense[]>([])
const totalGross = ref(0)
const totalNet = ref(0)
const categories = ref<Category[]>([])
const loading = ref(true)
const loadFailed = ref(false)
const loadRequestId = ref('')

const yearOptions = computed(() =>
  Array.from({ length: 8 }, (_, i) => String(thisYear + 1 - i)).map((y) => ({
    value: y,
    label: y,
  })),
)
const stateOptions = computed(() => [
  { value: ALL, label: t('expenses.filters.allStates') },
  { value: 'draft', label: t('expenses.state.draft') },
  { value: 'booked', label: t('expenses.state.booked') },
])
const categoryOptions = computed(() => [
  { value: ALL, label: t('expenses.filters.allCategories') },
  ...categories.value
    .filter((c) => c.kind === 'expense')
    .map((c) => ({ value: c.key, label: c.label_de })),
])
const categoryLabels = computed(
  () => new Map(categories.value.map((c) => [c.key, c.label_de] as const)),
)

async function toLogin(): Promise<void> {
  clearSession()
  await router.push({ name: 'login' })
}

// Each load takes a number. An answer that is not the newest request's is
// dropped, so a slow earlier filter cannot overwrite a newer list.
let latestRequest = 0

async function load(showSkeleton = true): Promise<void> {
  const request = ++latestRequest
  if (showSkeleton) {
    loading.value = true
  }
  loadFailed.value = false
  try {
    const answer = await apiClient.get<ListAnswer>('/expenses', {
      query: {
        year: year.value,
        state: state.value === ALL ? undefined : state.value,
        category: categoryKey.value === ALL ? undefined : categoryKey.value,
      },
    })
    if (request !== latestRequest) {
      return
    }
    expenses.value = answer.expenses
    totalGross.value = answer.total_gross_cents
    totalNet.value = answer.total_net_cents
  } catch (error) {
    if (request !== latestRequest) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    loadRequestId.value = isApiError(error) ? error.requestId : ''
    loadFailed.value = true
  } finally {
    if (request === latestRequest) {
      loading.value = false
    }
  }
}

async function loadCategories(): Promise<void> {
  try {
    const answer = await apiClient.get<{ categories: Category[] }>('/categories')
    categories.value = answer.categories
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
    }
    // Without the names the table shows the keys and the filter lists none.
  }
}

onMounted(async () => {
  await Promise.all([load(), loadCategories()])
})

// A filter change reloads the list without the skeleton, so the filters stay
// where the keyboard focus is.
watch([year, state, categoryKey], () => load(false))

function stateText(e: Expense): string {
  if (e.storno_of_id) {
    return t('expenses.state.storno')
  }
  return t(`expenses.state.${e.state}`)
}

function rowName(e: Expense): string {
  return e.supplier || e.description || t('expenses.noSupplier')
}

function openExpense(e: Expense): void {
  void router.push({ name: 'expense-capture', params: { id: e.id } })
}

function capture(): void {
  void router.push({ name: 'expense-capture' })
}
</script>

<template>
  <GovuePage width="shell" :title="t('expenses.title')" :lead="t('expenses.lead')">
    <div>
      <GovueButton variant="primary" @click="capture">{{ t('expenses.create') }}</GovueButton>
    </div>

    <form
      class="grid gap-4 sm:grid-cols-3"
      role="search"
      :aria-label="t('expenses.filters.label')"
      @submit.prevent
    >
      <GovueField :label="t('expenses.filters.year')">
        <GovueSelect v-model="year" :options="yearOptions" />
      </GovueField>
      <GovueField :label="t('expenses.filters.state')">
        <GovueSelect v-model="state" :options="stateOptions" />
      </GovueField>
      <GovueField :label="t('expenses.filters.category')">
        <GovueSelect v-model="categoryKey" :options="categoryOptions" />
      </GovueField>
    </form>

    <div v-if="loading" role="status">
      <span class="sr-only">{{ t('expenses.loading') }}</span>
      <GovueSkeleton :lines="4" />
    </div>

    <GovueErrorState
      v-else-if="loadFailed"
      :title="t('expenses.loadErrorTitle')"
      :message="t('expenses.loadError')"
      :request-id="loadRequestId || undefined"
      @retry="load()"
    />

    <GovueEmptyState
      v-else-if="expenses.length === 0"
      :title="t('expenses.emptyTitle')"
      :description="t('expenses.emptyDescription')"
    />

    <div v-else class="overflow-x-auto rounded-lg border border-border bg-surface">
      <table class="w-full text-left text-base">
        <caption class="sr-only">
          {{
            t('expenses.tableCaption')
          }}
        </caption>
        <thead class="border-b border-border bg-bg-subtle text-sm text-text-muted">
          <tr>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('expenses.columns.paidOn') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('expenses.columns.supplier') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('expenses.columns.category') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('expenses.columns.state') }}</th>
            <th scope="col" class="px-3 py-2 text-right font-medium">
              {{ t('expenses.columns.gross') }}
            </th>
            <th scope="col" class="px-3 py-2 text-right font-medium">
              {{ t('expenses.columns.net') }}
            </th>
            <th scope="col" class="px-3 py-2 font-medium">{{ t('expenses.columns.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in expenses" :key="e.id" class="border-b border-border last:border-b-0">
            <td class="px-3 py-2">{{ e.paid_on ?? '' }}</td>
            <th scope="row" class="px-3 py-2 font-medium">{{ rowName(e) }}</th>
            <td class="px-3 py-2">{{ categoryLabels.get(e.category) ?? e.category }}</td>
            <td class="px-3 py-2">
              <GovueBadge :kind="e.state === 'booked' ? 'success' : 'neutral'">
                {{ stateText(e) }}
              </GovueBadge>
            </td>
            <td class="px-3 py-2 text-right">{{ formatCents(e.gross_cents, locale) }}</td>
            <td class="px-3 py-2 text-right">{{ formatCents(e.net_cents, locale) }}</td>
            <td class="px-3 py-2">
              <GovueButton
                variant="quiet"
                :aria-label="t('expenses.openFor', { name: rowName(e) })"
                @click="openExpense(e)"
              >
                {{ t('expenses.open') }}
              </GovueButton>
            </td>
          </tr>
        </tbody>
        <tfoot class="border-t border-border bg-bg-subtle font-medium">
          <tr>
            <th scope="row" colspan="4" class="px-3 py-2">{{ t('expenses.totals.label') }}</th>
            <td class="px-3 py-2 text-right">
              <span class="sr-only">{{ t('expenses.totals.gross') }}: </span
              >{{ formatCents(totalGross, locale) }}
            </td>
            <td class="px-3 py-2 text-right">
              <span class="sr-only">{{ t('expenses.totals.net') }}: </span
              >{{ formatCents(totalNet, locale) }}
            </td>
            <td />
          </tr>
        </tfoot>
      </table>
    </div>
  </GovuePage>
</template>
