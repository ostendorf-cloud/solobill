<script setup lang="ts">
// PaymentsPanel shows the payments of one invoice chain (GET
// /api/v1/documents/{id}/payments) with the open amount, and lets the owner
// record a payment, record a refund or reverse a recording mistake. The
// server decides every rule again: this panel only saves the round trip for
// the amount cap of a refund and the date, and never computes a total. Money
// is read through money.ts into integer cents; a refund is sent as the
// negated amount, a reversal as the negated amount of the payment it undoes.
import {
  GovueBadge,
  GovueButton,
  GovueConfirmDialog,
  GovueDialog,
  GovueErrorState,
  GovueField,
  GovueInput,
  GovueSelect,
  GovueSkeleton,
  GovueTextarea,
  isApiError,
  useToasts,
} from '@ostendorf-cloud/govue'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import {
  PAYMENT_METHODS,
  berlinToday,
  canReverse,
  isIsoDate,
  isReversed,
  listPayments,
  negate,
  paymentKind,
  recordPayment,
  refundDue,
  type Payment,
  type PaymentBody,
  type PaymentsAnswer,
} from '../../documents'
import { centsToInput, formatCents, parseCents } from '../../money'
import { clearSession, loadSession, sessionFailed, sessionReady } from '../../session'

const props = defineProps<{
  /** Any issued document of the chain. The server resolves the root. */
  documentId: string
  /** Only the owner records payments (docs/api.md). */
  isOwner: boolean
}>()

const emit = defineEmits<{ loaded: [effectiveDocumentId: string] }>()

const { t, locale } = useI18n()
const router = useRouter()
const { notify } = useToasts()

const answer = ref<PaymentsAnswer | null>(null)
const loading = ref(true)
const loadFailed = ref(false)
const loadRequestId = ref('')

// An answer or a failure that belongs to a request that is no longer the
// newest, or that arrives after the panel is gone, changes nothing.
const unmounted = new AbortController()
onBeforeUnmount(() => unmounted.abort())
let latestRequest = 0

async function toLogin(): Promise<void> {
  clearSession()
  await router.push({ name: 'login' })
}

async function load(showSkeleton = true): Promise<void> {
  const request = ++latestRequest
  if (showSkeleton) {
    loading.value = true
  }
  loadFailed.value = false
  try {
    const result = await listPayments(props.documentId, { signal: unmounted.signal })
    if (request !== latestRequest || unmounted.signal.aborted) {
      return
    }
    answer.value = result
    emit('loaded', result.effective_document_id)
  } catch (error) {
    if (request !== latestRequest || unmounted.signal.aborted) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    loadRequestId.value = isApiError(error) ? error.requestId : ''
    loadFailed.value = true
  } finally {
    if (request === latestRequest && !unmounted.signal.aborted) {
      loading.value = false
    }
  }
}

onMounted(() => load())
watch(
  () => props.documentId,
  () => load(),
)

const payments = computed(() => answer.value?.payments ?? [])
const open = computed(() => answer.value?.open_cents ?? 0)
const due = computed(() => refundDue(open.value))

function moneyText(cents: number): string {
  return formatCents(cents, locale.value)
}

function methodText(payment: Payment): string {
  return t(`documents.payments.methods.${payment.method}`)
}

function kindText(payment: Payment): string {
  return t(`documents.payments.kinds.${paymentKind(payment)}`)
}

// The form of a payment or a refund, in one dialog.
type Mode = 'payment' | 'refund'
const formOpen = ref(false)
const mode = ref<Mode>('payment')
const form = reactive({ receivedOn: '', amount: '', method: 'bank_transfer', note: '' })
const errors = ref<Record<string, string>>({})
const saveError = ref('')
const saving = ref(false)

const methodOptions = computed(() =>
  PAYMENT_METHODS.map((m) => ({ value: m, label: t(`documents.payments.methods.${m}`) })),
)

function openForm(next: Mode): void {
  mode.value = next
  form.receivedOn = berlinToday()
  form.amount = next === 'refund' ? centsToInput(due.value, locale.value) : ''
  form.method = 'bank_transfer'
  form.note = ''
  errors.value = {}
  saveError.value = ''
  formOpen.value = true
  // The CSRF token comes from /me: try the session once more when it failed.
  if (!sessionReady.value) {
    void loadSession()
  }
}

/** focusFirstInvalid moves the focus to the first invalid field of the form.
 *  A timeout lets a closing dialog give the focus back first. */
async function focusFirstInvalid(formId: string): Promise<void> {
  await nextTick()
  setTimeout(() => {
    document.querySelector<HTMLElement>(`#${formId} [aria-invalid="true"]`)?.focus()
  }, 0)
}

const EXACT_REASONS: Record<string, string> = {
  required: 'required',
  'must not be 0': 'amountZero',
  'must be between 2000-01-01 and today': 'dateRange',
  'must be bank_transfer, cash, card, paypal or other': 'invalidMethod',
  'must not contain control or formatting characters other than a line break': 'noteChars',
  'no refund is due; undo a recording mistake with reverses_payment_id': 'noRefundDue',
  'must be a payment id': 'invalid',
}
const PATTERN_REASONS: [RegExp, string][] = [
  [/^must be a date/, 'invalidDate'],
  [/^must be at most \d+ in absolute value$/, 'amountTooLarge'],
  [/^must be at most \d+ characters$/, 'noteLong'],
  [/^a refund may pay back at most the refund due/, 'refundAboveDue'],
  [/^must be -?\d+, the negated amount/, 'reversalAmount'],
  [/^must be \d{4}-\d{2}-\d{2}, the received_on/, 'reversalDate'],
]

const FIELD_OF: Record<string, string> = {
  received_on: 'receivedOn',
  amount_cents: 'amount',
  method: 'method',
  note: 'note',
}

function serverErrors(details: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [path, reason] of Object.entries(details)) {
    const key =
      EXACT_REASONS[reason] ?? PATTERN_REASONS.find(([re]) => re.test(reason))?.[1] ?? 'invalid'
    out[FIELD_OF[path] ?? path] = t(`documents.payments.reasons.${key}`)
  }
  return out
}

/** buildBody reads the typed values. A refund is the negated amount; the cap
 *  of the refund due is checked here as well as on the server. */
function buildBody(): PaymentBody | null {
  const next: Record<string, string> = {}
  const received = form.receivedOn.trim()
  if (!isIsoDate(received)) {
    next.receivedOn = t('documents.payments.reasons.invalidDate')
  } else if (received > berlinToday()) {
    next.receivedOn = t('documents.payments.reasons.dateRange')
  }
  const cents = parseCents(form.amount)
  if (cents === null || cents === 0) {
    next.amount = t('documents.payments.reasons.badAmount')
  } else if (mode.value === 'refund' && cents > due.value) {
    next.amount = t('documents.payments.reasons.refundAboveDueAmount', {
      max: moneyText(due.value),
    })
  }
  errors.value = next
  if (Object.keys(next).length > 0 || cents === null) {
    return null
  }
  return {
    received_on: received,
    amount_cents: mode.value === 'refund' ? negate(cents) : cents,
    method: form.method as PaymentBody['method'],
    note: form.note.trim() === '' ? null : form.note,
    reverses_payment_id: null,
  }
}

/** submit sends a body. It answers whether the payment was recorded. */
async function send(body: PaymentBody): Promise<{ ok: boolean; error?: unknown }> {
  try {
    await recordPayment(props.documentId, body)
    return { ok: true }
  } catch (error) {
    return { ok: false, error }
  }
}

async function save(): Promise<void> {
  saveError.value = ''
  if (!sessionReady.value) {
    saveError.value = t('common.errors.session')
    void loadSession()
    return
  }
  const body = buildBody()
  if (!body) {
    saveError.value = t('documents.payments.saveError')
    void focusFirstInvalid('payment-form')
    return
  }
  saving.value = true
  const result = await send(body)
  if (unmounted.signal.aborted) {
    return
  }
  try {
    if (result.ok) {
      formOpen.value = false
      notify({
        kind: 'success',
        title: t(
          mode.value === 'refund' ? 'documents.payments.refunded' : 'documents.payments.saved',
        ),
      })
      await load(false)
      return
    }
    const error = result.error
    if (isApiError(error) && error.isUnauthenticated) {
      formOpen.value = false
      await toLogin()
      return
    }
    if (isApiError(error) && error.status === 400) {
      errors.value = serverErrors(error.details)
      saveError.value = t('documents.payments.saveError')
      void focusFirstInvalid('payment-form')
    } else if (isApiError(error) && error.status === 409) {
      saveError.value = t('documents.payments.conflict')
      await load(false)
    } else if (isApiError(error) && error.status === 403) {
      saveError.value = t('common.errors.forbidden')
    } else if (isApiError(error) && error.isTransport) {
      saveError.value = t('common.errors.network')
    } else if (isApiError(error) && error.status >= 500) {
      saveError.value = t('common.errors.server')
    } else {
      saveError.value = t('common.errors.generic')
    }
  } finally {
    saving.value = false
  }
}

// The reversal of a recording mistake: the counter entry carries the negated
// amount, the same day and the same method, and names the reversed row.
const reverseTarget = ref<Payment | null>(null)
const reverseOpen = ref(false)
const reversing = ref(false)

function askReverse(payment: Payment): void {
  reverseTarget.value = payment
  reverseOpen.value = true
  if (!sessionReady.value) {
    void loadSession()
  }
}

const reverseSubject = computed(() => {
  const p = reverseTarget.value
  return p ? `${p.received_on}, ${moneyText(p.amount_cents)}` : ''
})

async function confirmReverse(): Promise<void> {
  const target = reverseTarget.value
  if (!target) {
    return
  }
  if (!sessionReady.value) {
    notify({ kind: 'error', title: t('common.errors.session') })
    void loadSession()
    return
  }
  reversing.value = true
  const result = await send({
    received_on: target.received_on,
    amount_cents: negate(target.amount_cents),
    method: target.method,
    note: null,
    reverses_payment_id: target.id,
  })
  if (unmounted.signal.aborted) {
    return
  }
  try {
    if (result.ok) {
      reverseOpen.value = false
      notify({ kind: 'success', title: t('documents.payments.reversed') })
      await load(false)
      return
    }
    const error = result.error
    if (isApiError(error) && error.isUnauthenticated) {
      reverseOpen.value = false
      await toLogin()
      return
    }
    reverseOpen.value = false
    if (isApiError(error) && error.status === 409) {
      // Reversed meanwhile or not payable: the list is stale, not the click wrong.
      notify({ kind: 'error', title: t('documents.payments.conflict') })
      await load(false)
    } else if (isApiError(error) && error.status === 403) {
      notify({ kind: 'error', title: t('common.errors.forbidden') })
    } else {
      notify({ kind: 'error', title: t('documents.payments.reverseError') })
    }
  } finally {
    reversing.value = false
  }
}
</script>

<template>
  <section class="flex flex-col gap-4" aria-labelledby="payments-heading">
    <h2 id="payments-heading" class="text-xl font-semibold text-text">
      {{ t('documents.payments.title') }}
    </h2>

    <div v-if="loading" role="status">
      <span class="sr-only">{{ t('documents.payments.loading') }}</span>
      <GovueSkeleton :lines="3" />
    </div>

    <GovueErrorState
      v-else-if="loadFailed"
      :title="t('documents.payments.loadErrorTitle')"
      :message="t('documents.payments.loadError')"
      :request-id="loadRequestId || undefined"
      :heading-level="3"
      @retry="load()"
    />

    <template v-else-if="answer">
      <dl class="grid gap-3 rounded-lg border border-border bg-surface p-4 sm:grid-cols-2">
        <div>
          <dt class="text-sm text-text-muted">{{ t('documents.payments.paid') }}</dt>
          <dd class="text-lg font-semibold" data-testid="paid">
            {{ moneyText(answer.paid_cents) }}
          </dd>
        </div>
        <div>
          <dt class="text-sm text-text-muted">
            {{ open < 0 ? t('documents.payments.refundDue') : t('documents.payments.open') }}
          </dt>
          <dd class="text-lg font-semibold" data-testid="open">
            {{ moneyText(open < 0 ? due : open) }}
          </dd>
        </div>
      </dl>

      <p
        v-if="open < 0"
        role="status"
        class="rounded-md bg-warning-subtle px-3 py-2 text-warning-text"
      >
        {{ t('documents.payments.refundDueNote', { amount: moneyText(due) }) }}
      </p>
      <p v-else-if="open === 0" role="status" class="text-text-muted">
        {{ t('documents.payments.paidInFull') }}
      </p>

      <p v-if="payments.length === 0" class="text-text-muted">
        {{ t('documents.payments.empty') }}
      </p>
      <div v-else class="overflow-x-auto rounded-lg border border-border bg-surface">
        <table class="w-full text-left text-base">
          <caption class="sr-only">
            {{
              t('documents.payments.tableCaption')
            }}
          </caption>
          <thead class="border-b border-border bg-bg-subtle text-sm text-text-muted">
            <tr>
              <th scope="col" class="px-3 py-2 font-medium">
                {{ t('documents.payments.columns.receivedOn') }}
              </th>
              <th scope="col" class="px-3 py-2 font-medium">
                {{ t('documents.payments.columns.kind') }}
              </th>
              <th scope="col" class="px-3 py-2 font-medium">
                {{ t('documents.payments.columns.method') }}
              </th>
              <th scope="col" class="px-3 py-2 text-right font-medium">
                {{ t('documents.payments.columns.amount') }}
              </th>
              <th scope="col" class="px-3 py-2 font-medium">
                {{ t('documents.payments.columns.note') }}
              </th>
              <th v-if="isOwner" scope="col" class="px-3 py-2 font-medium">
                {{ t('documents.payments.columns.actions') }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="p in payments"
              :key="p.id"
              class="border-b border-border last:border-b-0"
              :data-payment="p.id"
            >
              <th scope="row" class="px-3 py-2 font-medium">{{ p.received_on }}</th>
              <td class="px-3 py-2">
                <GovueBadge :kind="paymentKind(p) === 'payment' ? 'success' : 'neutral'">
                  {{ kindText(p) }}
                </GovueBadge>
                <GovueBadge v-if="isReversed(p, payments)" kind="warning" class="ml-1">
                  {{ t('documents.payments.reversedMark') }}
                </GovueBadge>
              </td>
              <td class="px-3 py-2">{{ methodText(p) }}</td>
              <td class="px-3 py-2 text-right">{{ moneyText(p.amount_cents) }}</td>
              <td class="px-3 py-2 whitespace-pre-line">{{ p.note ?? '' }}</td>
              <td v-if="isOwner" class="px-3 py-2">
                <GovueButton
                  v-if="canReverse(p, payments)"
                  variant="quiet"
                  :disabled="!sessionReady"
                  :aria-label="
                    t('documents.payments.reverseFor', {
                      date: p.received_on,
                      amount: moneyText(p.amount_cents),
                    })
                  "
                  @click="askReverse(p)"
                >
                  {{ t('documents.payments.reverse') }}
                </GovueButton>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <p v-if="isOwner && sessionFailed && !sessionReady" role="alert" class="text-danger-text">
        {{ t('common.errors.session') }}
      </p>
      <div v-if="isOwner" class="flex flex-wrap gap-3">
        <GovueButton variant="primary" :disabled="!sessionReady" @click="openForm('payment')">
          {{ t('documents.payments.record') }}
        </GovueButton>
        <GovueButton
          variant="secondary"
          :disabled="!sessionReady || due === 0"
          @click="openForm('refund')"
        >
          {{ t('documents.payments.refund') }}
        </GovueButton>
      </div>
      <p v-else class="text-sm text-text-muted">{{ t('documents.payments.ownerOnly') }}</p>
    </template>

    <GovueDialog
      v-model:open="formOpen"
      size="lg"
      :title="
        t(mode === 'refund' ? 'documents.payments.refundTitle' : 'documents.payments.recordTitle')
      "
      :description="
        mode === 'refund'
          ? t('documents.payments.refundHint', { max: moneyText(due) })
          : t('documents.payments.recordHint')
      "
    >
      <form id="payment-form" class="flex flex-col gap-4" novalidate @submit.prevent="save">
        <GovueField
          :label="t('documents.payments.fields.receivedOn')"
          :hint="t('documents.payments.fields.receivedOnHint')"
          :error="errors.receivedOn"
          required
        >
          <GovueInput v-model="form.receivedOn" autocomplete="off" />
        </GovueField>
        <GovueField
          :label="t('documents.payments.fields.amount')"
          :hint="
            mode === 'refund'
              ? t('documents.payments.fields.amountRefundHint', { max: moneyText(due) })
              : t('documents.payments.fields.amountHint')
          "
          :error="errors.amount"
          required
        >
          <GovueInput v-model="form.amount" inputmode="decimal" autocomplete="off" />
        </GovueField>
        <GovueField :label="t('documents.payments.fields.method')" :error="errors.method">
          <GovueSelect v-model="form.method" :options="methodOptions" />
        </GovueField>
        <GovueField :label="t('documents.payments.fields.note')" :error="errors.note">
          <GovueTextarea v-model="form.note" :rows="3" />
        </GovueField>
        <p v-if="saveError" role="alert" class="text-danger-text">{{ saveError }}</p>
        <p v-if="sessionFailed && !sessionReady" role="alert" class="text-danger-text">
          {{ t('common.errors.session') }}
        </p>
      </form>

      <template #actions>
        <GovueButton variant="secondary" :disabled="saving" @click="formOpen = false">
          {{ t('documents.cancel') }}
        </GovueButton>
        <GovueButton
          type="submit"
          form="payment-form"
          variant="primary"
          :disabled="!sessionReady"
          :loading="saving"
          :loading-label="t('documents.payments.saving')"
        >
          {{
            t(
              mode === 'refund'
                ? 'documents.payments.refundSubmit'
                : 'documents.payments.recordSubmit',
            )
          }}
        </GovueButton>
      </template>
    </GovueDialog>

    <GovueConfirmDialog
      v-model:open="reverseOpen"
      :title="t('documents.payments.reverseTitle')"
      :message="t('documents.payments.reverseMessage')"
      :subject="reverseSubject"
      :confirm-label="t('documents.payments.reverse')"
      :cancel-label="t('documents.cancel')"
      :loading="reversing"
      @confirm="confirmReverse"
    />
  </section>
</template>
