<script setup lang="ts">
// ExpenseCaptureView is the receipt capture flow and the detail view of one
// expense. Without an id it starts with the upload step (10 MiB, PDF, JPEG,
// PNG or XML, a duplicate warning that may be passed), then shows the form
// beside a preview of the receipt. With an id it opens the saved expense.
// A draft can be saved, booked (owner) and deleted. A booked row is read only
// and can only be reversed with a storno (owner).
//
// The preview shows a JPEG or PNG inline from the download route. Every other
// type, a PDF included, is a download link: the route answers with
// `Content-Disposition: attachment` and `Content-Security-Policy: sandbox`,
// so no browser viewer would open it in a frame, and the app parses no PDF.
//
// The VAT regime comes from GET /api/v1/vat-regimes (any role): the period in
// force on the payment date decides. A Kleinunternehmer form has no VAT
// fields and sends net equal to gross, rate 0 and VAT 0.
import {
  GovueBadge,
  GovueButton,
  GovueConfirmDialog,
  GovueDialog,
  GovueErrorState,
  GovueField,
  GovueInput,
  GovuePage,
  GovueSelect,
  GovueSkeleton,
  GovueTextarea,
  ErrorCode,
  isApiError,
  useToasts,
} from '@ostendorf-cloud/govue'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { apiClient } from '../api'
import {
  checkReceiptFile,
  expenseFieldErrors,
  isInlineImage,
  isRegimeMissing,
  receiptContentUrl,
  regimeOn,
  signedCentsToInput,
  uploadReceipt,
  type Category,
  type Expense,
  type StoredFile,
  type VatPeriod,
} from '../expenses'
import { saveErrorText } from '../fieldErrors'
import { centsToInput, parseCents } from '../money'
import { clearSession, loadSession, me, sessionFailed, sessionReady } from '../session'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const { notify } = useToasts()

type Step = 'loading' | 'upload' | 'form' | 'failed'
const step = ref<Step>('loading')
const loadRequestId = ref('')

const expense = ref<Expense | null>(null)
const file = ref<StoredFile | null>(null)
const fileLoadFailed = ref(false)
const categories = ref<Category[]>([])
const periods = ref<VatPeriod[]>([])
const periodsState = ref<'loading' | 'ok' | 'failed'>('loading')

// The upload step.
const chosen = ref<File | null>(null)
const uploadError = ref('')
const uploading = ref(false)
const duplicates = ref<StoredFile[]>([])
const fileInputKey = ref(0)

// The form.
const form = reactive({
  paidOn: '',
  receiptDate: '',
  supplier: '',
  description: '',
  gross: '',
  rate: '',
  vat: '',
  category: '',
})
const isAsset = ref(false)
const errors = ref<Record<string, string>>({})
const saving = ref(false)
const saveError = ref('')
const snapshot = ref('')

const deleteOpen = ref(false)
const deleting = ref(false)
const bookOpen = ref(false)
const booking = ref(false)
const stornoOpen = ref(false)
const stornoPaidOn = ref('')
const stornoErrors = ref<Record<string, string>>({})
const stornoError = ref('')
const stornoing = ref(false)

const isOwner = computed(() => me.value?.role === 'owner')
const booked = computed(() => expense.value?.state === 'booked')
const locked = computed(() => booked.value)

const title = computed(() => {
  if (!expense.value) {
    return t('expenses.capture.title')
  }
  return booked.value ? t('expenses.capture.titleBooked') : t('expenses.capture.titleDraft')
})

const categoryOptions = computed(() =>
  categories.value
    .filter((c) => c.kind === 'expense')
    .map((c) => ({ value: c.key, label: c.label_de })),
)

function todayIso(): string {
  const now = new Date()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')
  return `${now.getFullYear()}-${month}-${day}`
}

// The regime of the payment date. Without a valid date the day of capture
// stands in for the display only: the server checks the real date.
const regime = computed(() => {
  if (periodsState.value !== 'ok') {
    return null
  }
  const date = /^\d{4}-\d{2}-\d{2}$/.test(form.paidOn.trim()) ? form.paidOn.trim() : todayIso()
  return regimeOn(periods.value, date)
})
const showVat = computed(() => regime.value !== 'kleinunternehmer')
const regimeNote = computed(() => {
  if (periodsState.value === 'failed') {
    return t('expenses.regime.loadFailed')
  }
  if (periodsState.value === 'loading') {
    return ''
  }
  return t(`expenses.regime.${regime.value ?? 'unknown'}`)
})

const netText = computed(() => {
  // A booked row, a storno row with negated amounts included, shows the
  // stored net amount with its sign.
  if (expense.value && booked.value) {
    return signedCentsToInput(expense.value.net_cents, locale.value)
  }
  const gross = parseCents(form.gross)
  if (gross === null) {
    return ''
  }
  const vat = showVat.value && form.vat.trim() !== '' ? parseCents(form.vat) : 0
  if (vat === null || vat > gross) {
    return ''
  }
  return centsToInput(gross - vat, locale.value)
})

const dirty = computed(() => JSON.stringify(form) !== snapshot.value)

function takeSnapshot(): void {
  snapshot.value = JSON.stringify(form)
}

function fillForm(e: Expense): void {
  form.paidOn = e.paid_on ?? ''
  form.receiptDate = e.receipt_date ?? ''
  form.supplier = e.supplier
  form.description = e.description
  form.gross = signedCentsToInput(e.gross_cents, locale.value)
  form.rate = e.vat_rate_permyriad === 0 ? '' : centsToInput(e.vat_rate_permyriad, locale.value)
  form.vat = e.vat_cents === 0 ? '' : signedCentsToInput(e.vat_cents, locale.value)
  form.category = e.category
  isAsset.value = e.is_asset
  takeSnapshot()
}

async function toLogin(): Promise<void> {
  clearSession()
  await router.push({ name: 'login' })
}

/** handleCommon answers the failures every call shares. It returns true when
 *  the failure is handled. */
async function handleCommon(error: unknown): Promise<boolean> {
  if (isApiError(error) && error.isUnauthenticated) {
    await toLogin()
    return true
  }
  if (isApiError(error) && error.status === 403) {
    notify({ kind: 'error', title: t('common.errors.forbidden') })
    return true
  }
  return false
}

async function loadSupport(): Promise<void> {
  const [cats, regimes] = await Promise.allSettled([
    apiClient.get<{ categories: Category[] }>('/categories'),
    apiClient.get<{ periods: VatPeriod[] }>('/vat-regimes'),
  ])
  for (const result of [cats, regimes]) {
    if (
      result.status === 'rejected' &&
      isApiError(result.reason) &&
      result.reason.isUnauthenticated
    ) {
      await toLogin()
      return
    }
  }
  if (cats.status === 'fulfilled') {
    categories.value = cats.value.categories
  }
  if (regimes.status === 'fulfilled') {
    periods.value = regimes.value.periods
    periodsState.value = 'ok'
  } else {
    periodsState.value = 'failed'
  }
}

async function loadFile(id: string): Promise<void> {
  fileLoadFailed.value = false
  try {
    file.value = await apiClient.get<StoredFile>(`/files/${id}`)
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    file.value = null
    fileLoadFailed.value = true
  }
}

async function loadExpense(id: string): Promise<void> {
  step.value = 'loading'
  try {
    const loaded = await apiClient.get<Expense>(`/expenses/${id}`)
    expense.value = loaded
    fillForm(loaded)
    if (loaded.file_id) {
      await loadFile(loaded.file_id)
    } else {
      file.value = null
    }
    step.value = 'form'
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    loadRequestId.value = isApiError(error) ? error.requestId : ''
    step.value = 'failed'
  }
}

function routeId(): string {
  const id = route.params.id
  return typeof id === 'string' ? id : ''
}

onMounted(async () => {
  if (!sessionReady.value && sessionFailed.value) {
    // The CSRF token is missing: try the session once more.
    void loadSession()
  }
  const support = loadSupport()
  const id = routeId()
  if (id) {
    await loadExpense(id)
  } else {
    step.value = 'upload'
  }
  await support
})

// A move to another expense (the address changed from outside) reloads. The
// id a first save puts into the address is the one already shown.
watch(
  () => route.params.id,
  (id) => {
    if (typeof id === 'string' && id !== '' && id !== expense.value?.id) {
      void loadExpense(id)
    }
  },
)

function chooseFile(event: Event): void {
  const input = event.target as HTMLInputElement
  const picked = input.files?.[0] ?? null
  chosen.value = null
  uploadError.value = ''
  if (!picked) {
    return
  }
  const refusal = checkReceiptFile(picked)
  if (refusal) {
    uploadError.value = t(`expenses.upload.errors.${refusal}`)
    return
  }
  chosen.value = picked
}

function uploadErrorText(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 413) {
      return t('expenses.upload.errors.serverTooLarge')
    }
    if (error.status === 415) {
      return t('expenses.upload.errors.serverUnsupported')
    }
    if (error.status === 503) {
      return t('expenses.upload.errors.storeUnavailable')
    }
    if (error.status === 403) {
      return t('common.errors.forbidden')
    }
    if (error.code === ErrorCode.timeout) {
      return t('expenses.upload.errors.timeout')
    }
    if (error.isTransport) {
      return t('common.errors.network')
    }
  }
  return t('expenses.upload.errors.failed')
}

// An upload that is still running when the view goes away is cancelled, and
// its answer, a 401 included, changes nothing afterwards.
const uploadAbort = new AbortController()
onBeforeUnmount(() => uploadAbort.abort())

/** focusFirstInvalid moves the focus to the first field of the form that
 *  carries an error. The alert texts stay as they are. A timeout lets a
 *  closing dialog give the focus back first. */
async function focusFirstInvalid(formId: string): Promise<void> {
  await nextTick()
  setTimeout(() => {
    document.querySelector<HTMLElement>(`#${formId} [aria-invalid="true"]`)?.focus()
  }, 0)
}

async function upload(): Promise<void> {
  if (!chosen.value) {
    return
  }
  if (!sessionReady.value) {
    notify({ kind: 'error', title: t('common.errors.session') })
    void loadSession()
    return
  }
  uploading.value = true
  uploadError.value = ''
  try {
    const result = await uploadReceipt(chosen.value, uploadAbort.signal)
    if (uploadAbort.signal.aborted) {
      return
    }
    file.value = result.file
    fileLoadFailed.value = false
    if (result.duplicates.length > 0) {
      duplicates.value = result.duplicates
    } else {
      step.value = 'form'
    }
  } catch (error) {
    if (uploadAbort.signal.aborted) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    uploadError.value = uploadErrorText(error)
  } finally {
    uploading.value = false
  }
}

function continueWithDuplicate(): void {
  duplicates.value = []
  step.value = 'form'
}

function chooseAnother(): void {
  duplicates.value = []
  file.value = null
  chosen.value = null
  fileInputKey.value++
}

function skipUpload(): void {
  file.value = null
  step.value = 'form'
}

const duplicateNames = computed(() => duplicates.value.map((d) => d.original_filename).join(', '))

function buildBody(): Record<string, unknown> | null {
  const next: Record<string, string> = {}
  const gross = parseCents(form.gross)
  if (gross === null || gross === 0) {
    next.gross = t('expenses.reasons.badAmount')
  }
  let rate = 0
  let vat = 0
  if (showVat.value) {
    if (form.rate.trim() !== '') {
      const parsed = parseCents(form.rate)
      if (parsed === null || parsed > 10000) {
        next.rate = t('expenses.reasons.badRate')
      } else {
        rate = parsed
      }
    }
    if (form.vat.trim() !== '') {
      const parsed = parseCents(form.vat)
      if (parsed === null) {
        next.vat = t('expenses.reasons.badAmount')
      } else {
        vat = parsed
      }
    }
    if (gross !== null && vat > gross && !next.vat) {
      next.vat = t('expenses.reasons.vatAboveGross')
    }
  }
  if (Object.keys(next).length > 0 || gross === null) {
    errors.value = next
    return null
  }
  return {
    paid_on: form.paidOn.trim() === '' ? null : form.paidOn.trim(),
    receipt_date: form.receiptDate.trim() === '' ? null : form.receiptDate.trim(),
    supplier: form.supplier,
    description: form.description,
    gross_cents: gross,
    // Integer subtraction: the VAT stays inside the gross amount.
    net_cents: gross - vat,
    vat_rate_permyriad: rate,
    vat_cents: vat,
    category: form.category,
    file_id: file.value?.id ?? null,
    is_asset: isAsset.value,
  }
}

function fieldErrorsOf(error: unknown): Record<string, string> {
  if (isRegimeMissing(error)) {
    return { paidOn: t('expenses.reasons.regimeMissing') }
  }
  if (isApiError(error) && error.status === 400) {
    return expenseFieldErrors(error.details, t)
  }
  return {}
}

async function save(): Promise<void> {
  saveError.value = ''
  errors.value = {}
  const body = buildBody()
  if (!body) {
    saveError.value = t('expenses.capture.saveError')
    void focusFirstInvalid('expense-form')
    return
  }
  saving.value = true
  try {
    if (expense.value) {
      expense.value = await apiClient.put<Expense>(`/expenses/${expense.value.id}`, body)
    } else {
      expense.value = await apiClient.post<Expense>('/expenses', body)
      await router.replace({ name: 'expense-capture', params: { id: expense.value.id } })
    }
    takeSnapshot()
    notify({ kind: 'success', title: t('expenses.capture.saved') })
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    errors.value = fieldErrorsOf(error)
    saveError.value = saveErrorText(error, t, 'expenses.capture.saveError')
    if (isRegimeMissing(error)) {
      saveError.value = t('expenses.capture.saveError')
    }
    void focusFirstInvalid('expense-form')
  } finally {
    saving.value = false
  }
}

function askBook(): void {
  bookOpen.value = true
}

async function confirmBook(): Promise<void> {
  if (!expense.value) {
    return
  }
  if (!sessionReady.value) {
    notify({ kind: 'error', title: t('common.errors.session') })
    void loadSession()
    return
  }
  booking.value = true
  errors.value = {}
  try {
    expense.value = await apiClient.post<Expense>(`/expenses/${expense.value.id}/book`)
    fillForm(expense.value)
    bookOpen.value = false
    notify({ kind: 'success', title: t('expenses.book.done') })
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      bookOpen.value = false
      await toLogin()
      return
    }
    bookOpen.value = false
    if (await handleCommon(error)) {
      return
    }
    if (isApiError(error) && error.status === 409) {
      notify({ kind: 'error', title: t('expenses.book.already') })
      await loadExpense(expense.value.id)
      return
    }
    errors.value = fieldErrorsOf(error)
    notify({ kind: 'error', title: t('expenses.book.error') })
    void focusFirstInvalid('expense-form')
  } finally {
    booking.value = false
  }
}

function askStorno(): void {
  stornoPaidOn.value = todayIso()
  stornoErrors.value = {}
  stornoError.value = ''
  stornoOpen.value = true
}

async function confirmStorno(): Promise<void> {
  if (!expense.value) {
    return
  }
  if (!sessionReady.value) {
    stornoError.value = t('common.errors.session')
    void loadSession()
    return
  }
  stornoing.value = true
  stornoErrors.value = {}
  stornoError.value = ''
  try {
    await apiClient.post(`/expenses/${expense.value.id}/storno`, {
      paid_on: stornoPaidOn.value.trim(),
    })
    stornoOpen.value = false
    notify({ kind: 'success', title: t('expenses.storno.done') })
    await router.push({ name: 'expenses' })
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      stornoOpen.value = false
      await toLogin()
      return
    }
    if (isApiError(error) && error.status === 403) {
      stornoError.value = t('common.errors.forbidden')
    } else if (isApiError(error) && error.status === 409) {
      stornoError.value = t('expenses.storno.conflict')
    } else {
      stornoErrors.value = fieldErrorsOf(error)
      stornoError.value = t('expenses.storno.error')
      void focusFirstInvalid('storno-form')
    }
  } finally {
    stornoing.value = false
  }
}

async function confirmDelete(): Promise<void> {
  if (!expense.value) {
    return
  }
  if (!sessionReady.value) {
    notify({ kind: 'error', title: t('common.errors.session') })
    void loadSession()
    return
  }
  deleting.value = true
  try {
    await apiClient.del(`/expenses/${expense.value.id}`)
    deleteOpen.value = false
    notify({ kind: 'success', title: t('expenses.delete.done') })
    await router.push({ name: 'expenses' })
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      deleteOpen.value = false
      await toLogin()
      return
    }
    deleteOpen.value = false
    if (await handleCommon(error)) {
      return
    }
    notify({ kind: 'error', title: t('expenses.delete.error') })
  } finally {
    deleting.value = false
  }
}

function openOriginal(): void {
  if (expense.value?.storno_of_id) {
    void router.push({ name: 'expense-capture', params: { id: expense.value.storno_of_id } })
  }
}

function back(): void {
  void router.push({ name: 'expenses' })
}
</script>

<template>
  <GovuePage width="shell" :title="title" :lead="t('expenses.capture.lead')">
    <div>
      <GovueButton variant="quiet" @click="back">{{ t('expenses.capture.back') }}</GovueButton>
    </div>

    <div v-if="step === 'loading'" role="status">
      <span class="sr-only">{{ t('expenses.capture.loading') }}</span>
      <GovueSkeleton :lines="4" />
    </div>

    <GovueErrorState
      v-else-if="step === 'failed'"
      :title="t('expenses.capture.loadErrorTitle')"
      :message="t('expenses.capture.loadError')"
      :request-id="loadRequestId || undefined"
      @retry="loadExpense(routeId())"
    />

    <!-- Step 1: the receipt. -->
    <section v-else-if="step === 'upload'" class="flex max-w-xl flex-col gap-4">
      <h2 class="text-lg font-semibold">{{ t('expenses.upload.title') }}</h2>
      <p class="text-text-muted">{{ t('expenses.upload.lead') }}</p>

      <div
        v-if="duplicates.length > 0"
        role="alert"
        class="flex flex-col gap-3 rounded-lg border border-border bg-warning-subtle p-4 text-warning-text"
      >
        <h3 class="font-semibold">{{ t('expenses.upload.duplicateTitle') }}</h3>
        <p>{{ t('expenses.upload.duplicateMessage', { names: duplicateNames }) }}</p>
        <div class="flex flex-wrap gap-2">
          <GovueButton variant="primary" @click="continueWithDuplicate">
            {{ t('expenses.upload.duplicateContinue') }}
          </GovueButton>
          <GovueButton variant="secondary" @click="chooseAnother">
            {{ t('expenses.upload.duplicateChoose') }}
          </GovueButton>
        </div>
      </div>

      <template v-else>
        <!-- govue has no file control: the native input sits inside the
             field, which supplies the label, the hint and the error. -->
        <GovueField
          v-slot="{ id, describedBy }"
          :label="t('expenses.upload.file')"
          :hint="t('expenses.upload.fileHint')"
          :error="uploadError"
        >
          <input
            :id="id"
            :key="fileInputKey"
            type="file"
            class="block w-full min-h-11 rounded-md border border-border-strong bg-surface px-3 py-2 text-base text-text"
            accept=".pdf,.jpg,.jpeg,.png,.xml,application/pdf,image/jpeg,image/png,application/xml,text/xml"
            :aria-describedby="describedBy"
            :aria-invalid="uploadError ? 'true' : undefined"
            @change="chooseFile"
          />
        </GovueField>
        <p v-if="sessionFailed && !sessionReady" role="alert" class="text-danger-text">
          {{ t('common.errors.session') }}
        </p>
        <div class="flex flex-wrap gap-2">
          <GovueButton
            variant="primary"
            :disabled="!chosen || !sessionReady"
            :loading="uploading"
            :loading-label="t('expenses.upload.uploading')"
            @click="upload"
          >
            {{ t('expenses.upload.button') }}
          </GovueButton>
          <GovueButton variant="secondary" :disabled="uploading" @click="skipUpload">
            {{ t('expenses.upload.skip') }}
          </GovueButton>
        </div>
      </template>
    </section>

    <!-- Step 2: the preview beside the form. -->
    <div v-else class="grid gap-6 lg:grid-cols-2">
      <section class="flex flex-col gap-3" :aria-label="t('expenses.preview.title')">
        <h2 class="text-lg font-semibold">{{ t('expenses.preview.title') }}</h2>
        <p v-if="fileLoadFailed" role="alert" class="text-danger-text">
          {{ t('expenses.preview.loadError') }}
        </p>
        <p v-else-if="!file" class="text-text-muted">{{ t('expenses.preview.noReceipt') }}</p>
        <template v-else>
          <img
            v-if="isInlineImage(file.content_type)"
            :src="receiptContentUrl(file.id)"
            :alt="t('expenses.preview.imageAlt', { name: file.original_filename })"
            class="max-h-[40rem] w-full rounded-lg border border-border bg-surface object-contain"
          />
          <p v-else class="text-text-muted">{{ t('expenses.preview.notShown') }}</p>
          <a
            :href="receiptContentUrl(file.id)"
            download
            class="text-accent underline underline-offset-2"
          >
            {{ t('expenses.preview.download', { name: file.original_filename }) }}
          </a>
        </template>
      </section>

      <section class="flex flex-col gap-4">
        <div v-if="expense" class="flex flex-wrap items-center gap-2">
          <GovueBadge :kind="booked ? 'success' : 'neutral'">
            {{
              expense.storno_of_id
                ? t('expenses.state.storno')
                : t(`expenses.state.${expense.state}`)
            }}
          </GovueBadge>
          <span v-if="expense.booked_at" class="text-sm text-text-muted">
            {{ t('expenses.capture.bookedAt', { date: expense.booked_at.slice(0, 10) }) }}
          </span>
        </div>
        <p v-if="booked && !expense?.storno_of_id" class="text-text-muted">
          {{ t('expenses.capture.bookedNote') }}
        </p>
        <template v-if="expense?.storno_of_id">
          <p class="text-text-muted">{{ t('expenses.capture.stornoNote') }}</p>
          <div>
            <GovueButton variant="quiet" @click="openOriginal">
              {{ t('expenses.capture.openOriginal') }}
            </GovueButton>
          </div>
        </template>

        <form
          id="expense-form"
          class="flex flex-col gap-4"
          novalidate
          :aria-label="t('expenses.capture.formCaption')"
          @submit.prevent="save"
        >
          <GovueField
            :label="t('expenses.fields.paidOn')"
            :hint="t('expenses.fields.paidOnHint')"
            :error="errors.paidOn"
            :disabled="locked"
          >
            <GovueInput v-model="form.paidOn" autocomplete="off" inputmode="numeric" />
          </GovueField>
          <GovueField
            :label="t('expenses.fields.receiptDate')"
            :hint="t('expenses.fields.receiptDateHint')"
            :error="errors.receiptDate"
            :disabled="locked"
          >
            <GovueInput v-model="form.receiptDate" autocomplete="off" inputmode="numeric" />
          </GovueField>
          <GovueField
            :label="t('expenses.fields.supplier')"
            :error="errors.supplier"
            :disabled="locked"
          >
            <GovueInput v-model="form.supplier" autocomplete="off" />
          </GovueField>
          <GovueField
            :label="t('expenses.fields.description')"
            :error="errors.description"
            :disabled="locked"
          >
            <GovueTextarea v-model="form.description" :rows="3" />
          </GovueField>

          <p v-if="regimeNote" class="rounded-md bg-bg-subtle p-3 text-sm text-text-muted">
            {{ regimeNote }}
          </p>

          <GovueField
            :label="t('expenses.fields.gross')"
            :hint="t('expenses.fields.grossHint')"
            :error="errors.gross"
            :disabled="locked"
            required
          >
            <GovueInput v-model="form.gross" autocomplete="off" inputmode="decimal" />
          </GovueField>
          <template v-if="showVat">
            <GovueField
              :label="t('expenses.fields.rate')"
              :hint="t('expenses.fields.rateHint')"
              :error="errors.rate"
              :disabled="locked"
            >
              <GovueInput v-model="form.rate" autocomplete="off" inputmode="decimal" />
            </GovueField>
            <GovueField
              :label="t('expenses.fields.vat')"
              :hint="t('expenses.fields.vatHint')"
              :error="errors.vat"
              :disabled="locked"
            >
              <GovueInput v-model="form.vat" autocomplete="off" inputmode="decimal" />
            </GovueField>
            <GovueField :label="t('expenses.fields.net')">
              <GovueInput :model-value="netText" readonly />
            </GovueField>
          </template>
          <GovueField
            :label="t('expenses.fields.category')"
            :error="errors.category"
            :disabled="locked"
          >
            <GovueSelect
              v-model="form.category"
              :options="categoryOptions"
              :placeholder="t('expenses.fields.categoryPlaceholder')"
            />
          </GovueField>

          <p v-if="errors.file" role="alert" class="text-danger-text">{{ errors.file }}</p>
          <p v-if="saveError" role="alert" class="text-danger-text">{{ saveError }}</p>
          <p v-if="sessionFailed && !sessionReady" role="alert" class="text-danger-text">
            {{ t('common.errors.session') }}
          </p>
          <p v-if="expense && !booked && dirty" class="text-sm text-text-muted">
            {{ t('expenses.capture.unsaved') }}
          </p>
        </form>

        <div class="flex flex-wrap gap-2">
          <GovueButton
            v-if="!locked"
            type="submit"
            form="expense-form"
            variant="primary"
            :disabled="!sessionReady"
            :loading="saving"
            :loading-label="t('expenses.capture.saving')"
          >
            {{ t('expenses.capture.save') }}
          </GovueButton>
          <GovueButton
            v-if="expense && !booked && isOwner"
            variant="secondary"
            :disabled="!sessionReady || dirty"
            @click="askBook"
          >
            {{ t('expenses.book.action') }}
          </GovueButton>
          <GovueButton
            v-if="expense && !booked"
            variant="quiet"
            :disabled="!sessionReady"
            @click="deleteOpen = true"
          >
            {{ t('expenses.delete.action') }}
          </GovueButton>
          <GovueButton
            v-if="expense && booked && !expense.storno_of_id && isOwner"
            variant="secondary"
            :disabled="!sessionReady"
            @click="askStorno"
          >
            {{ t('expenses.storno.action') }}
          </GovueButton>
        </div>
      </section>
    </div>

    <GovueConfirmDialog
      v-model:open="bookOpen"
      :title="t('expenses.book.title')"
      :message="t('expenses.book.message')"
      :subject="expense?.supplier || expense?.description || ''"
      :confirm-label="t('expenses.book.confirm')"
      :cancel-label="t('expenses.cancel')"
      :loading="booking"
      @confirm="confirmBook"
    />

    <GovueConfirmDialog
      v-model:open="deleteOpen"
      :title="t('expenses.delete.title')"
      :message="t('expenses.delete.message')"
      :subject="expense?.supplier || expense?.description || ''"
      :confirm-label="t('expenses.delete.action')"
      :cancel-label="t('expenses.cancel')"
      :loading="deleting"
      @confirm="confirmDelete"
    />

    <GovueDialog v-model:open="stornoOpen" :title="t('expenses.storno.title')">
      <form id="storno-form" class="flex flex-col gap-4" novalidate @submit.prevent="confirmStorno">
        <p>{{ t('expenses.storno.message') }}</p>
        <GovueField
          :label="t('expenses.storno.paidOn')"
          :hint="t('expenses.storno.paidOnHint')"
          :error="stornoErrors.paidOn"
          required
        >
          <GovueInput v-model="stornoPaidOn" autocomplete="off" inputmode="numeric" />
        </GovueField>
        <p v-if="stornoError" role="alert" class="text-danger-text">{{ stornoError }}</p>
      </form>
      <template #actions>
        <GovueButton variant="secondary" :disabled="stornoing" @click="stornoOpen = false">
          {{ t('expenses.cancel') }}
        </GovueButton>
        <GovueButton
          type="submit"
          form="storno-form"
          variant="primary"
          :disabled="!sessionReady"
          :loading="stornoing"
        >
          {{ t('expenses.storno.confirm') }}
        </GovueButton>
      </template>
    </GovueDialog>
  </GovuePage>
</template>
