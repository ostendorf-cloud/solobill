<script setup lang="ts">
// AssetFormDialog creates an asset from a booked expense with the asset flag
// (POST /api/v1/assets) or edits one (PUT /api/v1/assets/{id}). The owner
// enters the useful life. The browser proposes the cost from the expense and
// the VAT regime of its payment date; the server checks it and is the
// authority, as it is for the schedule, which is never computed here.
import {
  isApiError,
  useToasts,
  GovueButton,
  GovueDialog,
  GovueField,
  GovueInput,
  GovueSelect,
} from '@ostendorf-cloud/govue'
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { apiClient } from '../api'
import {
  assetFieldErrors,
  germanYear,
  GROUPS,
  isExpenseTaken,
  isRegimeMissing,
  METHODS,
  type Asset,
} from '../assets'
import { saveErrorText } from '../fieldErrors'
import { regimeOn, type Expense, type VatPeriod } from '../expenses'
import { centsToInput, formatCents, parseCents } from '../money'
import { clearSession, loadSession, sessionFailed, sessionReady } from '../session'
import UnverifiedNote from './UnverifiedNote.vue'

const props = defineProps<{
  /** The asset to edit, or null to create one. */
  asset: Asset | null
  /** Expense ids that are an asset already: they are not offered. */
  takenExpenseIds?: readonly string[]
}>()
const emit = defineEmits<{ saved: [asset: Asset] }>()
const open = defineModel<boolean>('open', { default: false })

const { t, locale } = useI18n()
const router = useRouter()
const { notify } = useToasts()

// Requests still running when the dialog goes away are cancelled, and their
// answers, a 401 included, change nothing afterwards.
const abort = new AbortController()
onBeforeUnmount(() => abort.abort())

// The payment year starts at the current German year, like the server's.
const thisYear = germanYear()
const year = ref(String(thisYear))
const expenses = ref<Expense[]>([])
const periods = ref<VatPeriod[]>([])
const expensesState = ref<'idle' | 'loading' | 'ok' | 'failed'>('idle')

const form = reactive({
  expenseId: '',
  name: '',
  acquiredOn: '',
  cost: '',
  method: 'linear',
  group: 'office_equipment',
  usefulLife: '',
})
const errors = ref<Record<string, string>>({})
const saveError = ref('')
const saving = ref(false)
const costProposed = ref(false)

const editing = computed(() => props.asset !== null)

const yearOptions = computed(() =>
  Array.from({ length: 8 }, (_, i) => String(thisYear - i)).map((y) => ({ value: y, label: y })),
)
const methodOptions = computed(() =>
  METHODS.map((m) => ({ value: m, label: t(`assets.method.${m}`) })),
)
const groupOptions = computed(() =>
  GROUPS.map((g) => ({ value: g, label: t(`assets.group.${g}`) })),
)

/** offered are the booked, non storno expenses with the asset flag that are
 *  not an asset yet. */
const offered = computed(() =>
  expenses.value.filter(
    (e) =>
      e.state === 'booked' &&
      e.is_asset &&
      e.storno_of_id === null &&
      !(props.takenExpenseIds ?? []).includes(e.id),
  ),
)
const expenseOptions = computed(() =>
  offered.value.map((e) => ({
    value: e.id,
    label: `${e.paid_on ?? ''} ${e.supplier || e.description} ${formatCents(e.gross_cents, locale.value)}`,
  })),
)

let latestLoad = 0

async function loadExpenses(): Promise<void> {
  const request = ++latestLoad
  expensesState.value = 'loading'
  try {
    const [list, regimes] = await Promise.all([
      apiClient.get<{ expenses: Expense[] }>('/expenses', {
        query: { year: year.value, state: 'booked' },
        signal: abort.signal,
      }),
      periods.value.length > 0
        ? Promise.resolve({ periods: periods.value })
        : apiClient
            .get<{ periods: VatPeriod[] }>('/vat-regimes', { signal: abort.signal })
            .catch((error: unknown) => {
              if (isApiError(error) && error.isUnauthenticated) {
                throw error
              }
              // Without the regimes the cost is not proposed, the owner types it.
              return { periods: [] as VatPeriod[] }
            }),
    ])
    if (abort.signal.aborted || request !== latestLoad) {
      return
    }
    expenses.value = list.expenses
    periods.value = regimes.periods
    expensesState.value = 'ok'
  } catch (error) {
    if (abort.signal.aborted || request !== latestLoad) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    expensesState.value = 'failed'
  }
}

async function toLogin(): Promise<void> {
  if (abort.signal.aborted) {
    return
  }
  clearSession()
  await router.push({ name: 'login' })
}

function reset(): void {
  errors.value = {}
  saveError.value = ''
  costProposed.value = false
  const a = props.asset
  form.expenseId = ''
  form.name = a?.name ?? ''
  form.acquiredOn = a?.acquired_on ?? ''
  form.cost = a ? centsToInput(a.cost_basis_cents, locale.value) : ''
  form.method = a?.method ?? 'linear'
  form.group = a?.group ?? 'office_equipment'
  form.usefulLife = a?.useful_life_months === null || !a ? '' : String(a.useful_life_months)
}

watch(open, (isOpen) => {
  if (!isOpen) {
    return
  }
  reset()
  // The mutating calls need the CSRF token from /me: try once more when the
  // shell's own load failed.
  if (!sessionReady.value) {
    void loadSession()
  }
  if (!editing.value) {
    expenses.value = []
    void loadExpenses()
  }
})

watch(year, () => {
  if (open.value && !editing.value) {
    form.expenseId = ''
    void loadExpenses()
  }
})

// Choosing an expense proposes name, date and cost from it. The cost is the
// net amount under the standard regime and the gross amount for a small
// business owner, picked by the regime of the payment date. Nothing is
// computed: both figures come from the expense.
watch(
  () => form.expenseId,
  (id) => {
    const expense = offered.value.find((e) => e.id === id)
    if (!expense || editing.value) {
      return
    }
    form.name = expense.supplier || expense.description
    form.acquiredOn = expense.paid_on ?? ''
    const regime = expense.paid_on ? regimeOn(periods.value, expense.paid_on) : null
    const cents =
      regime === 'kleinunternehmer'
        ? expense.gross_cents
        : regime === 'standard'
          ? expense.net_cents
          : null
    form.cost = cents === null ? '' : centsToInput(cents, locale.value)
    costProposed.value = cents !== null
  },
)

/** focusFirstInvalid moves the focus to the first field of the form that
 *  carries an error. A timeout lets a closing dialog give the focus back
 *  first. */
async function focusFirstInvalid(): Promise<void> {
  await nextTick()
  setTimeout(() => {
    document.querySelector<HTMLElement>('#asset-form [aria-invalid="true"]')?.focus()
  }, 0)
}

function buildBody(): Record<string, unknown> | null {
  const next: Record<string, string> = {}
  if (!editing.value && form.expenseId === '') {
    next.expense = t('assets.reasons.noExpense')
  }
  if (form.name.trim() === '') {
    next.name = t('common.fieldErrors.required')
  }
  const cost = parseCents(form.cost)
  if (cost === null || cost === 0) {
    next.cost = t('assets.reasons.badAmount')
  }
  let life: number | null = null
  if (form.method === 'linear') {
    if (/^\d{1,3}$/.test(form.usefulLife.trim())) {
      life = Number.parseInt(form.usefulLife.trim(), 10)
    } else {
      next.usefulLife = t('assets.reasons.badUsefulLife')
    }
  }
  if (Object.keys(next).length > 0 || cost === null) {
    errors.value = next
    return null
  }
  const a = props.asset
  return {
    name: form.name,
    acquired_on: form.acquiredOn.trim(),
    cost_basis_cents: cost,
    method: form.method,
    group: form.group,
    useful_life_months: life,
    expense_id: a ? a.expense_id : form.expenseId,
    // An imported asset keeps its opening book value as it is.
    opening_book_value_cents: a?.opening_book_value_cents ?? null,
    opening_book_value_year: a?.opening_book_value_year ?? null,
  }
}

function fieldErrorsOf(error: unknown): Record<string, string> {
  if (isRegimeMissing(error)) {
    return { expense: t('assets.reasons.regimeMissing') }
  }
  if (isApiError(error) && error.status === 400) {
    return assetFieldErrors(error.details, t, locale.value)
  }
  return {}
}

async function save(): Promise<void> {
  saveError.value = ''
  errors.value = {}
  if (!sessionReady.value) {
    // No CSRF token yet: the request would only be refused.
    saveError.value = t('common.errors.session')
    void loadSession()
    return
  }
  const body = buildBody()
  if (!body) {
    saveError.value = t('assets.form.saveError')
    void focusFirstInvalid()
    return
  }
  saving.value = true
  try {
    const saved = props.asset
      ? await apiClient.put<Asset>(`/assets/${props.asset.id}`, body, { signal: abort.signal })
      : await apiClient.post<Asset>('/assets', body, { signal: abort.signal })
    if (abort.signal.aborted) {
      return
    }
    open.value = false
    notify({ kind: 'success', title: t('assets.form.saved') })
    emit('saved', saved)
  } catch (error) {
    if (abort.signal.aborted) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    errors.value = fieldErrorsOf(error)
    if (isApiError(error) && error.status === 409) {
      if (!editing.value || isExpenseTaken(error)) {
        errors.value = { expense: t('assets.form.expenseTaken') }
        saveError.value = t('assets.form.expenseTaken')
      } else {
        saveError.value = t('assets.form.conflictEdit')
      }
    } else {
      saveError.value = saveErrorText(error, t, 'assets.form.saveError')
    }
    void focusFirstInvalid()
  } finally {
    if (!abort.signal.aborted) {
      saving.value = false
    }
  }
}
</script>

<template>
  <GovueDialog
    v-model:open="open"
    size="lg"
    :title="editing ? t('assets.form.editTitle') : t('assets.form.createTitle')"
  >
    <form id="asset-form" class="flex flex-col gap-4" novalidate @submit.prevent="save">
      <UnverifiedNote />

      <template v-if="!editing">
        <GovueField :label="t('assets.form.year')">
          <GovueSelect v-model="year" :options="yearOptions" />
        </GovueField>
        <p v-if="expensesState === 'loading'" role="status" class="text-text-muted">
          {{ t('assets.form.expensesLoading') }}
        </p>
        <p v-else-if="expensesState === 'failed'" role="alert" class="text-danger-text">
          {{ t('assets.form.expensesFailed') }}
        </p>
        <p
          v-else-if="expensesState === 'ok' && expenseOptions.length === 0"
          class="text-text-muted"
        >
          {{ t('assets.form.noExpenses') }}
        </p>
        <GovueField
          :label="t('assets.form.expense')"
          :hint="t('assets.form.expenseHint')"
          :error="errors.expense"
          required
        >
          <GovueSelect
            v-model="form.expenseId"
            :options="expenseOptions"
            :placeholder="t('assets.form.expensePlaceholder')"
          />
        </GovueField>
      </template>
      <p v-else-if="asset?.expense_id" class="text-text-muted">
        {{ t('assets.form.linkedExpense') }}
      </p>

      <GovueField :label="t('assets.form.name')" :error="errors.name" required>
        <GovueInput v-model="form.name" autocomplete="off" />
      </GovueField>
      <GovueField
        :label="t('assets.form.acquiredOn')"
        :hint="t('assets.form.acquiredOnHint')"
        :error="errors.acquiredOn"
      >
        <GovueInput v-model="form.acquiredOn" autocomplete="off" inputmode="numeric" />
      </GovueField>
      <GovueField
        :label="t('assets.form.cost')"
        :hint="costProposed ? t('assets.form.regimeNote') : t('assets.form.costHint')"
        :error="errors.cost"
        required
      >
        <GovueInput v-model="form.cost" autocomplete="off" inputmode="decimal" />
      </GovueField>
      <GovueField :label="t('assets.form.method')" :error="errors.method">
        <GovueSelect v-model="form.method" :options="methodOptions" />
      </GovueField>
      <GovueField :label="t('assets.form.group')" :error="errors.group">
        <GovueSelect v-model="form.group" :options="groupOptions" />
      </GovueField>
      <GovueField
        v-if="form.method === 'linear'"
        :label="t('assets.form.usefulLife')"
        :hint="t('assets.form.usefulLifeHint')"
        :error="errors.usefulLife"
        required
      >
        <GovueInput v-model="form.usefulLife" autocomplete="off" inputmode="numeric" />
      </GovueField>
      <p v-if="errors.opening" role="alert" class="text-danger-text">{{ errors.opening }}</p>
      <p v-if="saveError" role="alert" class="text-danger-text">{{ saveError }}</p>
      <p v-if="sessionFailed && !sessionReady" role="alert" class="text-danger-text">
        {{ t('common.errors.session') }}
      </p>
    </form>

    <template #actions>
      <GovueButton variant="secondary" :disabled="saving" @click="open = false">
        {{ t('assets.form.cancel') }}
      </GovueButton>
      <GovueButton
        type="submit"
        form="asset-form"
        variant="primary"
        :disabled="!sessionReady"
        :loading="saving"
        :loading-label="t('assets.form.saving')"
      >
        {{ t('assets.form.save') }}
      </GovueButton>
    </template>
  </GovueDialog>
</template>
