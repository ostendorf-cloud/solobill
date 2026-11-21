<script setup lang="ts">
// DocumentEditorView edits a draft offer, invoice or correction. Lines come
// from the catalogue or are typed ad hoc, lines and the document can carry a
// discount, and the totals are whatever POST /api/v1/documents/preview
// answers: the browser computes no money. Typed amounts are read into
// integer cents by money.ts. The preview is debounced, abortable and guarded
// against a stale answer. Field errors of a save or an issue land at their
// field.
import {
  GovueButton,
  GovueConfirmDialog,
  GovueErrorState,
  GovueField,
  GovueInput,
  GovuePage,
  GovueSelect,
  GovueSkeleton,
  GovueTextarea,
  isApiError,
  useToasts,
} from '@ostendorf-cloud/govue'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'

import { apiClient } from '../api'
import DocumentIssue from '../components/documents/DocumentIssue.vue'
import {
  docToForm,
  documentFieldErrors,
  emptyForm,
  emptyLine,
  formToBody,
  type DiscountMode,
  type DocumentForm,
  type LineForm,
  type MappedErrors,
} from '../documentForm'
import {
  LINE_UNITS,
  anySignal,
  berlinToday,
  createDocument,
  formatPermyriad,
  getDocument,
  previewDocument,
  updateDocument,
  type Doc,
  type PreviewAnswer,
  type Regime,
} from '../documents'
import { regimeOn, type VatPeriod } from '../expenses'
import { saveErrorText } from '../fieldErrors'
import { centsToInput, formatCents } from '../money'
import { clearSession, loadSession, me, sessionFailed, sessionReady } from '../session'

interface Customer {
  id: string
  name: string
  document_language: 'de' | 'en'
}

interface CatalogueItem {
  id: string
  name: string
  description: string | null
  unit_price_cents: number
  unit: string
}

/** PREVIEW_DEBOUNCE_MS is how long the form rests before the totals are
 *  asked for again. */
const PREVIEW_DEBOUNCE_MS = 300
const ADHOC = 'adhoc'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const { notify } = useToasts()

const step = ref<'loading' | 'form' | 'issued' | 'failed' | 'notFound'>('loading')
const loadRequestId = ref('')
const doc = ref<Doc | null>(null)
const form = reactive<DocumentForm>(emptyForm('invoice', ''))
const snapshot = ref('')

const customers = ref<Customer[]>([])
const items = ref<CatalogueItem[]>([])
const periods = ref<VatPeriod[]>([])

const errors = ref<Record<string, string>>({})
const previewErrors = ref<Record<string, string>>({})
const saving = ref(false)
const saveError = ref('')

const isOwner = computed(() => me.value?.role === 'owner')
const isCorrection = computed(() => form.type === 'correction')
const isOffer = computed(() => form.type === 'offer')
const dirty = computed(() => JSON.stringify(form) !== snapshot.value)
// A correction keeps the customer, the regime and the language of the
// invoice it corrects.
const headerLocked = computed(() => isCorrection.value)

const title = computed(() => {
  if (doc.value) {
    return t('documents.editor.titleEdit', { kind: t(`documents.type.${form.type}`) })
  }
  return t(`documents.editor.titleNew.${form.type}`)
})

// The view going away ends every read, the preview included, and an answer or
// a 401 that still arrives changes nothing and sends nobody anywhere.
const unmounted = new AbortController()
onBeforeUnmount(() => {
  unmounted.abort()
  clearTimeout(previewTimer)
})

async function toLogin(): Promise<void> {
  clearSession()
  await router.push({ name: 'login' })
}

function takeSnapshot(): void {
  snapshot.value = JSON.stringify(form)
}

// Filling the form from a stored draft is no edit of the regime: the rates
// the draft carries stay as they are.
let applying = false

function fill(next: DocumentForm): void {
  applying = true
  Object.assign(form, next)
  applying = false
  takeSnapshot()
}

watch(
  () => form.regime,
  (regime) => {
    if (applying) {
      return
    }
    // Under Kleinunternehmer every rate is 0 by law; leaving it makes the
    // owner state each rate again.
    for (const line of form.lines) {
      if (regime === 'kleinunternehmer') {
        line.vatRate = '0'
      } else if (line.vatRate.trim() === '0') {
        line.vatRate = ''
      }
    }
  },
  { flush: 'sync' },
)

watch(
  () => form.customerId,
  (id) => {
    if (applying || doc.value) {
      return
    }
    // A new document starts in the language of its customer.
    const found = customers.value.find((c) => c.id === id)
    if (found) {
      form.language = found.document_language
    }
  },
  { flush: 'sync' },
)

function routeId(): string {
  const id = route.params.id
  return typeof id === 'string' ? id : ''
}

function newType(): 'invoice' | 'offer' {
  return route.query.type === 'offer' ? 'offer' : 'invoice'
}

async function loadSupport(): Promise<void> {
  const [c, i, p] = await Promise.allSettled([
    apiClient.get<{ customers: Customer[] }>('/customers', { signal: unmounted.signal }),
    apiClient.get<{ items: CatalogueItem[] }>('/catalogue-items', { signal: unmounted.signal }),
    apiClient.get<{ periods: VatPeriod[] }>('/vat-regimes', { signal: unmounted.signal }),
  ])
  if (unmounted.signal.aborted) {
    return
  }
  for (const result of [c, i, p]) {
    if (
      result.status === 'rejected' &&
      isApiError(result.reason) &&
      result.reason.isUnauthenticated
    ) {
      await toLogin()
      return
    }
  }
  if (c.status === 'fulfilled') {
    customers.value = c.value.customers
  }
  if (i.status === 'fulfilled') {
    items.value = i.value.items
  }
  if (p.status === 'fulfilled') {
    periods.value = p.value.periods
  }
}

let latestLoad = 0

async function loadDraft(id: string): Promise<void> {
  const request = ++latestLoad
  step.value = 'loading'
  const stale = (): boolean => request !== latestLoad || unmounted.signal.aborted
  try {
    const loaded = await getDocument(id, { signal: unmounted.signal })
    if (stale()) {
      return
    }
    doc.value = loaded
    if (loaded.state === 'issued') {
      step.value = 'issued'
      return
    }
    fill(docToForm(loaded, locale.value))
    errors.value = {}
    step.value = 'form'
    void schedulePreview(0)
  } catch (error) {
    if (stale()) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    if (isApiError(error) && error.status === 404) {
      step.value = 'notFound'
      return
    }
    loadRequestId.value = isApiError(error) ? error.requestId : ''
    step.value = 'failed'
  }
}

async function start(): Promise<void> {
  if (!sessionReady.value && sessionFailed.value) {
    // The CSRF token is missing: try the session once more.
    void loadSession()
  }
  const support = loadSupport()
  const id = routeId()
  if (id) {
    await Promise.all([support, loadDraft(id)])
    return
  }
  await support
  if (unmounted.signal.aborted) {
    return
  }
  doc.value = null
  fill(emptyForm(newType(), regimeOn(periods.value, berlinToday()) ?? ''))
  // The default regime fixes the rate of the first line.
  if (form.regime === 'kleinunternehmer') {
    for (const line of form.lines) {
      line.vatRate = '0'
    }
  }
  takeSnapshot()
  step.value = 'form'
  void schedulePreview(0)
}

onMounted(() => start())

// A move to another draft (for example a new correction after a changed base)
// reloads. The id a first save puts into the address is the one already shown.
watch(
  () => route.params.id,
  (id) => {
    if (typeof id === 'string' && id !== '' && id !== doc.value?.id) {
      void loadDraft(id)
    }
  },
)

// ---- the live totals ----

type PreviewState = 'idle' | 'calculating' | 'ok' | 'incomplete' | 'invalid' | 'failed'
const previewState = ref<PreviewState>('idle')
const preview = ref<PreviewAnswer | null>(null)
let previewTimer: ReturnType<typeof setTimeout> | undefined
let previewRequest = 0
let previewAbort: AbortController | undefined

// True from an edit until the preview request for it starts: the totals on
// screen are then older than the form.
const previewPending = ref(false)

function schedulePreview(delay = PREVIEW_DEBOUNCE_MS): void {
  clearTimeout(previewTimer)
  previewPending.value = true
  previewTimer = setTimeout(() => void runPreview(), delay)
}

async function runPreview(): Promise<void> {
  previewPending.value = false
  const request = ++previewRequest
  previewAbort?.abort()
  const { body } = formToBody(form, t, false)
  if (!body) {
    preview.value = null
    previewErrors.value = {}
    previewState.value = 'incomplete'
    return
  }
  const controller = new AbortController()
  previewAbort = controller
  previewState.value = 'calculating'
  const stale = (): boolean => request !== previewRequest || unmounted.signal.aborted
  try {
    const answer = await previewDocument(body, {
      signal: anySignal([unmounted.signal, controller.signal]),
    })
    if (stale()) {
      return
    }
    preview.value = answer
    previewErrors.value = {}
    previewState.value = 'ok'
  } catch (error) {
    if (stale()) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    preview.value = null
    if (isApiError(error) && error.status === 400 && Object.keys(error.details).length > 0) {
      previewErrors.value = documentFieldErrors(error.details, t).fields
      previewState.value = 'invalid'
    } else {
      previewErrors.value = {}
      previewState.value = 'failed'
    }
  }
}

// Any edit starts the debounce again; a stale request is aborted when the next
// one starts.
watch(
  form,
  () => {
    if (step.value === 'form') {
      schedulePreview()
    }
  },
  { deep: true },
)

const shown = computed(() => ({ ...previewErrors.value, ...errors.value }))

function lineNet(i: number): string {
  const line = preview.value?.lines[i]
  return line ? formatCents(line.net_after_line_discount_cents, locale.value) : ''
}

function money(cents: number): string {
  return formatCents(cents, locale.value)
}

// ---- options ----

const customerOptions = computed(() => {
  const options = customers.value.map((c) => ({ value: c.id, label: c.name }))
  if (form.customerId && !options.some((o) => o.value === form.customerId)) {
    options.push({ value: form.customerId, label: t('documents.editor.customerUnknown') })
  }
  return options
})
const regimeOptions = computed(() => [
  { value: 'standard', label: t('documents.regime.standard') },
  { value: 'kleinunternehmer', label: t('documents.regime.kleinunternehmer') },
])
const languageOptions = computed(() => [
  { value: 'de', label: t('documents.language.de') },
  { value: 'en', label: t('documents.language.en') },
])
const unitOptions = computed(() =>
  LINE_UNITS.map((u) => ({ value: u, label: t(`documents.units.${u}`) })),
)
const discountOptions = computed(() => [
  { value: 'none', label: t('documents.editor.discountMode.none') },
  { value: 'percent', label: t('documents.editor.discountMode.percent') },
  { value: 'amount', label: t('documents.editor.discountMode.amount') },
])

function catalogueOptions(line: LineForm) {
  const options = [
    { value: ADHOC, label: t('documents.editor.adhoc') },
    ...items.value.map((i) => ({ value: i.id, label: i.name })),
  ]
  if (line.catalogueItemId && !items.value.some((i) => i.id === line.catalogueItemId)) {
    options.push({ value: line.catalogueItemId, label: t('documents.editor.catalogueUnknown') })
  }
  return options
}

function pickCatalogue(line: LineForm, value: string): void {
  if (value === ADHOC) {
    line.catalogueItemId = null
    return
  }
  line.catalogueItemId = value
  const item = items.value.find((i) => i.id === value)
  if (!item) {
    return
  }
  line.description = item.description ? `${item.name}\n${item.description}` : item.name
  if (LINE_UNITS.some((u) => u === item.unit)) {
    line.unit = item.unit
  }
  line.unitPrice = centsToInput(item.unit_price_cents, locale.value)
}

function addLine(): void {
  form.lines.push(emptyLine(form.regime))
}

function removeLine(index: number): void {
  form.lines.splice(index, 1)
}

function setDiscountMode(
  target: { discountMode: DiscountMode; discountValue: string },
  value: string,
): void {
  target.discountMode = value as DiscountMode
  if (value === 'none') {
    target.discountValue = ''
  }
}

// ---- save ----

/** focusFirstInvalid moves the focus to the first field of the form that
 *  carries an error. A timeout lets a closing dialog give the focus back
 *  first. */
async function focusFirstInvalid(): Promise<void> {
  await nextTick()
  setTimeout(() => {
    document.querySelector<HTMLElement>('#document-form [aria-invalid="true"]')?.focus()
  }, 0)
}

async function save(): Promise<boolean> {
  saveError.value = ''
  errors.value = {}
  if (!sessionReady.value) {
    saveError.value = t('common.errors.session')
    void loadSession()
    return false
  }
  const { body, errors: typed } = formToBody(form, t, true)
  if (!body) {
    errors.value = typed
    saveError.value = t('documents.editor.saveError')
    void focusFirstInvalid()
    return false
  }
  saving.value = true
  try {
    const saved = doc.value ? await updateDocument(doc.value.id, body) : await createDocument(body)
    if (unmounted.signal.aborted) {
      return false
    }
    const created = doc.value === null
    doc.value = saved
    takeSnapshot()
    notify({ kind: 'success', title: t('documents.editor.saved') })
    if (created) {
      await router.replace({ name: 'document-edit', params: { id: saved.id } })
    }
    return true
  } catch (error) {
    if (unmounted.signal.aborted) {
      return false
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return false
    }
    if (isApiError(error) && error.status === 400) {
      errors.value = documentFieldErrors(error.details, t).fields
    }
    saveError.value = saveErrorText(error, t, 'documents.editor.saveError')
    void focusFirstInvalid()
    return false
  } finally {
    if (!unmounted.signal.aborted) {
      saving.value = false
    }
  }
}

/** saveForIssue makes sure the stored draft is the one on screen before the
 *  confirmation opens. */
async function saveForIssue(): Promise<Doc | null> {
  if (doc.value && !dirty.value) {
    return doc.value
  }
  // The answer of the save carries the totals the server stored: the
  // confirmation shows those, not the ones of the preview.
  return (await save()) ? doc.value : null
}

/** issueBlockedHint says why Issue is off: it waits for settled totals. */
const issueBlockedHint = computed(() => {
  let reason: string | null = null
  if (
    previewPending.value ||
    previewState.value === 'calculating' ||
    previewState.value === 'idle'
  ) {
    reason = 'calculating'
  } else if (previewState.value !== 'ok') {
    reason = previewState.value
  }
  return reason ? t(`documents.issue.blocked.${reason}`) : ''
})

// What a screen reader hears: only the settled grand total or the failure,
// never every step of a preview cycle.
const liveSummary = computed(() => {
  if (previewPending.value) {
    return ''
  }
  if (previewState.value === 'ok' && preview.value) {
    return t('documents.editor.liveTotal', {
      total: formatCents(preview.value.totals.grand_total_cents, locale.value),
    })
  }
  if (previewState.value === 'failed') {
    return t('documents.editor.previewFailed')
  }
  if (previewState.value === 'invalid') {
    return t('documents.editor.previewInvalid')
  }
  return ''
})

// ---- leaving with unsaved changes ----

const leaveOpen = ref(false)
let leaveTarget: string | null = null
let leaveConfirmed = false

onBeforeRouteLeave((to) => {
  if (leaveConfirmed || !dirty.value || step.value !== 'form') {
    return true
  }
  leaveTarget = to.fullPath
  leaveOpen.value = true
  return false
})

function confirmLeave(): void {
  leaveConfirmed = true
  leaveOpen.value = false
  if (leaveTarget) {
    void router.push(leaveTarget)
  }
}

// A reload or a closed tab with unsaved changes asks the browser to confirm.
function warnBeforeUnload(event: BeforeUnloadEvent): void {
  event.preventDefault()
  event.returnValue = ''
}

watch(
  dirty,
  (isDirty) => {
    if (isDirty) {
      window.addEventListener('beforeunload', warnBeforeUnload)
    } else {
      window.removeEventListener('beforeunload', warnBeforeUnload)
    }
  },
  { immediate: true },
)
onBeforeUnmount(() => window.removeEventListener('beforeunload', warnBeforeUnload))

function onIssueFields(mapped: MappedErrors): void {
  errors.value = mapped.fields
  if (Object.keys(mapped.fields).length > 0) {
    void focusFirstInvalid()
  }
}

function onIssued(issued: Doc): void {
  void router.push({ name: 'document-detail', params: { id: issued.id } })
}

function openNewCorrection(id: string): void {
  void router.push({ name: 'document-edit', params: { id } })
}

const issueSubject = computed(() => {
  const name = customers.value.find((c) => c.id === form.customerId)?.name
  const kind = t(`documents.type.${form.type}`)
  return name ? `${kind}, ${name}` : kind
})

const regimeModel = computed({
  get: () => form.regime,
  set: (value: string) => {
    form.regime = value as Regime | ''
  },
})
</script>

<template>
  <GovuePage width="shell" :title="title" :lead="t('documents.editor.lead')">
    <div v-if="step === 'loading'" role="status">
      <span class="sr-only">{{ t('documents.editor.loading') }}</span>
      <GovueSkeleton :lines="6" />
    </div>

    <GovueErrorState
      v-else-if="step === 'failed'"
      :title="t('documents.editor.loadErrorTitle')"
      :message="t('documents.editor.loadError')"
      :request-id="loadRequestId || undefined"
      @retry="loadDraft(routeId())"
    />

    <GovueErrorState
      v-else-if="step === 'notFound'"
      :title="t('documents.detail.notFoundTitle')"
      :message="t('documents.detail.notFound')"
      :retry-label="t('documents.detail.backToList')"
      @retry="router.push({ name: 'documents' })"
    />

    <div v-else-if="step === 'issued' && doc" class="flex flex-col gap-3">
      <p role="status" class="rounded-md bg-warning-subtle px-3 py-2 text-warning-text">
        {{ t('documents.editor.issuedReadOnly') }}
      </p>
      <p>
        <RouterLink :to="{ name: 'document-detail', params: { id: doc.id } }" class="underline">
          {{ t('documents.editor.openDocument') }}
        </RouterLink>
      </p>
    </div>

    <template v-else>
      <p
        v-if="isCorrection"
        role="status"
        class="rounded-md bg-info-subtle px-3 py-2 text-info-text"
      >
        {{ t('documents.editor.correctionNote') }}
      </p>

      <form id="document-form" class="flex flex-col gap-6" novalidate @submit.prevent="save">
        <fieldset class="flex flex-col gap-4">
          <legend class="mb-2 text-xl font-semibold text-text">
            {{ t('documents.editor.header') }}
          </legend>

          <GovueField
            :label="t('documents.fields.customer')"
            :error="shown.customer"
            required
            :disabled="headerLocked"
          >
            <GovueSelect
              v-model="form.customerId"
              :options="customerOptions"
              :placeholder="t('documents.editor.customerPlaceholder')"
            />
          </GovueField>

          <div class="grid gap-4 sm:grid-cols-2">
            <GovueField
              :label="t('documents.fields.regime')"
              :error="shown.regime"
              :disabled="headerLocked"
            >
              <GovueSelect
                v-model="regimeModel"
                :options="regimeOptions"
                :placeholder="t('documents.editor.regimePlaceholder')"
              />
            </GovueField>
            <GovueField
              :label="t('documents.fields.language')"
              :error="shown.language"
              :disabled="headerLocked"
            >
              <GovueSelect v-model="form.language" :options="languageOptions" />
            </GovueField>
          </div>

          <div v-if="!isOffer" class="grid gap-4 sm:grid-cols-2">
            <GovueField
              :label="t('documents.editor.serviceDate')"
              :hint="t('documents.editor.dateHint')"
              :error="shown.serviceDate"
            >
              <GovueInput v-model="form.serviceDate" autocomplete="off" />
            </GovueField>
            <GovueField
              :label="t('documents.editor.serviceDateTo')"
              :hint="t('documents.editor.serviceDateToHint')"
              :error="shown.serviceDateTo"
            >
              <GovueInput v-model="form.serviceDateTo" autocomplete="off" />
            </GovueField>
          </div>

          <div class="grid gap-4 sm:grid-cols-2">
            <GovueField
              v-if="!isOffer"
              :label="t('documents.fields.dueDate')"
              :hint="t('documents.editor.dateHint')"
              :error="shown.dueDate"
            >
              <GovueInput v-model="form.dueDate" autocomplete="off" />
            </GovueField>
            <GovueField
              v-else
              :label="t('documents.fields.validUntil')"
              :hint="t('documents.editor.dateHint')"
              :error="shown.validUntil"
            >
              <GovueInput v-model="form.validUntil" autocomplete="off" />
            </GovueField>
            <GovueField
              :label="t('documents.fields.customerReference')"
              :error="shown.customerReference"
            >
              <GovueInput v-model="form.customerReference" autocomplete="off" />
            </GovueField>
          </div>
        </fieldset>

        <section class="flex flex-col gap-4" aria-labelledby="lines-heading">
          <h2 id="lines-heading" class="text-xl font-semibold text-text">
            {{ t('documents.editor.lines') }}
          </h2>
          <p v-if="shown.lines" role="alert" class="text-danger-text">{{ shown.lines }}</p>

          <fieldset
            v-for="(line, i) in form.lines"
            :key="line.key"
            class="flex flex-col gap-4 rounded-lg border border-border bg-surface p-4"
          >
            <legend class="px-1 font-medium text-text">
              {{ t('documents.editor.lineLegend', { n: i + 1 }) }}
            </legend>

            <GovueField
              :label="`${t('documents.editor.catalogue')} (${i + 1})`"
              :error="shown[`line.${i}.catalogue`]"
            >
              <GovueSelect
                :model-value="line.catalogueItemId ?? ADHOC"
                :options="catalogueOptions(line)"
                @update:model-value="pickCatalogue(line, $event)"
              />
            </GovueField>
            <GovueField
              :label="`${t('documents.lines.description')} (${i + 1})`"
              :error="shown[`line.${i}.description`]"
              required
            >
              <GovueTextarea v-model="line.description" :rows="2" />
            </GovueField>

            <div class="grid gap-4 sm:grid-cols-4">
              <GovueField
                :label="`${t('documents.lines.quantity')} (${i + 1})`"
                :hint="t('documents.editor.quantityHint')"
                :error="shown[`line.${i}.quantity`]"
                required
              >
                <GovueInput v-model="line.quantity" inputmode="decimal" autocomplete="off" />
              </GovueField>
              <GovueField
                :label="`${t('documents.lines.unit')} (${i + 1})`"
                :error="shown[`line.${i}.unit`]"
              >
                <GovueSelect v-model="line.unit" :options="unitOptions" />
              </GovueField>
              <GovueField
                :label="`${t('documents.lines.unitPrice')} (${i + 1})`"
                :hint="t('documents.editor.unitPriceHint')"
                :error="shown[`line.${i}.unitPrice`]"
                required
              >
                <GovueInput v-model="line.unitPrice" inputmode="decimal" autocomplete="off" />
              </GovueField>
              <GovueField
                :label="`${t('documents.editor.vatRatePercent')} (${i + 1})`"
                :hint="
                  form.regime === 'kleinunternehmer'
                    ? t('documents.editor.vatRateFixed')
                    : t('documents.editor.vatRateHint')
                "
                :error="shown[`line.${i}.vatRate`]"
                :required="form.regime !== 'kleinunternehmer'"
              >
                <GovueInput
                  v-model="line.vatRate"
                  inputmode="decimal"
                  autocomplete="off"
                  :readonly="form.regime === 'kleinunternehmer'"
                />
              </GovueField>
            </div>

            <div class="grid gap-4 sm:grid-cols-2">
              <GovueField
                :label="`${t('documents.editor.lineDiscount')} (${i + 1})`"
                :error="line.discountMode === 'none' ? shown[`line.${i}.discount`] : undefined"
              >
                <GovueSelect
                  :model-value="line.discountMode"
                  :options="discountOptions"
                  @update:model-value="setDiscountMode(line, $event)"
                />
              </GovueField>
              <GovueField
                v-if="line.discountMode !== 'none'"
                :label="`${t(`documents.editor.discountValue.${line.discountMode}`)} (${i + 1})`"
                :error="shown[`line.${i}.discount`]"
              >
                <GovueInput v-model="line.discountValue" inputmode="decimal" autocomplete="off" />
              </GovueField>
            </div>

            <div class="flex flex-wrap items-center justify-between gap-3">
              <p v-if="lineNet(i)" class="text-text-muted" :data-testid="`line-net-${i}`">
                {{ t('documents.editor.lineNet') }}: {{ lineNet(i) }}
              </p>
              <span v-else />
              <GovueButton
                variant="quiet"
                :aria-label="t('documents.editor.removeLine', { n: i + 1 })"
                @click="removeLine(i)"
              >
                {{ t('documents.editor.remove') }}
              </GovueButton>
            </div>
          </fieldset>

          <div>
            <GovueButton variant="secondary" @click="addLine">
              {{ t('documents.editor.addLine') }}
            </GovueButton>
          </div>
        </section>

        <fieldset class="flex flex-col gap-4">
          <legend class="mb-2 text-xl font-semibold text-text">
            {{ t('documents.editor.discountTitle') }}
          </legend>
          <div class="grid gap-4 sm:grid-cols-2">
            <GovueField
              :label="t('documents.editor.documentDiscount')"
              :error="form.discountMode === 'none' ? shown.discount : undefined"
            >
              <GovueSelect
                :model-value="form.discountMode"
                :options="discountOptions"
                @update:model-value="setDiscountMode(form, $event)"
              />
            </GovueField>
            <GovueField
              v-if="form.discountMode !== 'none'"
              :label="t(`documents.editor.discountValue.${form.discountMode}`)"
              :error="shown.discount"
            >
              <GovueInput v-model="form.discountValue" inputmode="decimal" autocomplete="off" />
            </GovueField>
          </div>
        </fieldset>

        <fieldset class="flex flex-col gap-4">
          <legend class="mb-2 text-xl font-semibold text-text">
            {{ t('documents.editor.texts') }}
          </legend>
          <GovueField :label="t('documents.fields.paymentTerms')" :error="shown.paymentTerms">
            <GovueTextarea v-model="form.paymentTerms" :rows="3" />
          </GovueField>
          <GovueField :label="t('documents.fields.notes')" :error="shown.notes">
            <GovueTextarea v-model="form.notes" :rows="3" />
          </GovueField>
        </fieldset>

        <section
          class="flex flex-col gap-2 rounded-lg border border-border bg-bg-subtle p-4"
          aria-labelledby="preview-heading"
        >
          <h2 id="preview-heading" class="text-xl font-semibold text-text">
            {{ t('documents.editor.totals') }}
          </h2>
          <!-- The settled total or the failure is announced once; the steps of
               a preview cycle are not. -->
          <p class="sr-only" role="status" aria-live="polite" data-testid="live-summary">
            {{ liveSummary }}
          </p>
          <div data-testid="preview">
            <p v-if="previewState === 'calculating'" class="text-text-muted">
              {{ t('documents.editor.calculating') }}
            </p>
            <p v-else-if="previewState === 'incomplete'" class="text-text-muted">
              {{ t('documents.editor.previewIncomplete') }}
            </p>
            <p v-else-if="previewState === 'invalid'" class="text-danger-text">
              {{ t('documents.editor.previewInvalid') }}
            </p>
            <div v-else-if="previewState === 'failed'" class="flex flex-col items-start gap-2">
              <p class="text-danger-text">{{ t('documents.editor.previewFailed') }}</p>
              <GovueButton variant="secondary" @click="runPreview">
                {{ t('documents.editor.previewRetry') }}
              </GovueButton>
            </div>
            <dl
              v-if="preview && (previewState === 'ok' || previewState === 'calculating')"
              class="grid max-w-md grid-cols-[1fr_auto] gap-x-6 gap-y-1"
              data-testid="preview-totals"
            >
              <dt>{{ t('documents.totals.net') }}</dt>
              <dd class="text-right">{{ money(preview.totals.net_total_cents) }}</dd>
              <template v-if="preview.totals.document_discount_cents > 0">
                <dt>{{ t('documents.editor.documentDiscount') }}</dt>
                <dd class="text-right">−{{ money(preview.totals.document_discount_cents) }}</dd>
                <dt>{{ t('documents.totals.netAfterDiscount') }}</dt>
                <dd class="text-right">
                  {{ money(preview.totals.net_after_document_discount_cents) }}
                </dd>
              </template>
              <template v-if="form.regime !== 'kleinunternehmer'">
                <template v-for="bucket in preview.totals.vat" :key="bucket.rate_permyriad">
                  <dt>
                    {{
                      t('documents.totals.vatAt', {
                        rate: formatPermyriad(bucket.rate_permyriad, locale),
                      })
                    }}
                  </dt>
                  <dd class="text-right">{{ money(bucket.vat_cents) }}</dd>
                </template>
              </template>
              <dt class="font-semibold">{{ t('documents.totals.grand') }}</dt>
              <dd class="text-right font-semibold">
                {{ money(preview.totals.grand_total_cents) }}
              </dd>
            </dl>
          </div>
          <p v-if="form.regime === 'kleinunternehmer'" class="text-sm text-text-muted">
            {{ t('documents.totals.kleinunternehmer') }}
          </p>
        </section>

        <p v-if="saveError" role="alert" class="text-danger-text">{{ saveError }}</p>
        <p v-if="sessionFailed && !sessionReady" role="alert" class="text-danger-text">
          {{ t('common.errors.session') }}
        </p>

        <div class="flex flex-wrap gap-3">
          <GovueButton
            type="submit"
            variant="primary"
            :disabled="!sessionReady"
            :loading="saving"
            :loading-label="t('documents.editor.saving')"
          >
            {{ t('documents.editor.save') }}
          </GovueButton>
          <GovueButton
            v-if="doc"
            variant="secondary"
            @click="router.push({ name: 'document-detail', params: { id: doc.id } })"
          >
            {{ t('documents.editor.toDocument') }}
          </GovueButton>
          <GovueButton variant="secondary" @click="router.push({ name: 'documents' })">
            {{ t('documents.editor.toList') }}
          </GovueButton>
        </div>
      </form>

      <section v-if="doc" class="flex flex-col gap-2" aria-labelledby="issue-heading">
        <h2 id="issue-heading" class="text-xl font-semibold text-text">
          {{ t('documents.editor.issueTitle') }}
        </h2>
        <p class="text-text-muted">{{ t('documents.editor.issueHint') }}</p>
        <DocumentIssue
          :document-id="doc.id"
          :document-type="doc.type"
          :corrects-document-id="doc.corrects_document_id"
          :subject="issueSubject"
          :is-owner="isOwner"
          :before-issue="saveForIssue"
          :blocked-hint="issueBlockedHint"
          @fields="onIssueFields"
          @issued="onIssued"
          @new-correction="openNewCorrection"
          @discarded="router.push({ name: 'documents' })"
        />
      </section>
    </template>

    <GovueConfirmDialog
      v-model:open="leaveOpen"
      :title="t('documents.editor.leave.title')"
      :message="t('documents.editor.leave.message')"
      :subject="title"
      :confirm-label="t('documents.editor.leave.confirm')"
      :cancel-label="t('documents.editor.leave.cancel')"
      @confirm="confirmLeave"
    />
  </GovuePage>
</template>
