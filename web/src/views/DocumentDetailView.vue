<script setup lang="ts">
// DocumentDetailView shows one document read only: a draft with its actions
// (edit, issue, discard), an issued invoice or correction with its chain
// status, its payments and the correction action, an offer with the
// offer-to-invoice action. Totals are what the API answers, never computed
// here. A draft shows no number; an issued document shows the stored one.
import {
  GovueBadge,
  GovueButton,
  GovueConfirmDialog,
  GovueErrorState,
  GovuePage,
  GovueSkeleton,
  isApiError,
  useToasts,
} from '@ostendorf-cloud/govue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import { apiClient } from '../api'
import DocumentIssue from '../components/documents/DocumentIssue.vue'
import PaymentsPanel from '../components/documents/PaymentsPanel.vue'
import {
  classifyIssueError,
  createCorrection,
  deleteDocument,
  formatPermyriad,
  formatQuantity,
  getDocument,
  offerToInvoice,
  type Doc,
  type DocumentLine,
  type IssueFailure,
} from '../documents'
import { formatCents } from '../money'
import { clearSession, loadSession, me, sessionFailed, sessionReady } from '../session'

interface CustomerInfo {
  id: string
  name: string
  number: string | null
  deleted_at?: string | null
}

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const { notify } = useToasts()

const doc = ref<Doc | null>(null)
const customer = ref<CustomerInfo | null>(null)
const loading = ref(true)
const loadFailed = ref(false)
const notFound = ref(false)
const loadRequestId = ref('')
const effectiveId = ref<string | null>(null)

const isOwner = computed(() => me.value?.role === 'owner')
const isDraft = computed(() => doc.value?.state === 'draft')
const isIssued = computed(() => doc.value?.state === 'issued')
const isChainDoc = computed(() => doc.value !== null && doc.value.type !== 'offer')
const superseded = computed(() => doc.value?.chain_status === 'superseded')

const unmounted = new AbortController()
onBeforeUnmount(() => unmounted.abort())

async function toLogin(): Promise<void> {
  clearSession()
  await router.push({ name: 'login' })
}

function routeId(): string {
  const id = route.params.id
  return typeof id === 'string' ? id : ''
}

let latestRequest = 0

async function load(): Promise<void> {
  const request = ++latestRequest
  const id = routeId()
  loading.value = true
  loadFailed.value = false
  notFound.value = false
  effectiveId.value = null
  const stale = (): boolean => request !== latestRequest || unmounted.signal.aborted
  try {
    const loaded = await getDocument(id, { signal: unmounted.signal })
    if (stale()) {
      return
    }
    doc.value = loaded
    // The customer may be deleted: the read answers for it all the same. A
    // failure only leaves the name out.
    try {
      const found = await apiClient.get<CustomerInfo>(
        `/customers/${encodeURIComponent(loaded.customer_id)}`,
        { signal: unmounted.signal },
      )
      if (!stale()) {
        customer.value = found
      }
    } catch (error) {
      if (stale()) {
        return
      }
      if (isApiError(error) && error.isUnauthenticated) {
        await toLogin()
        return
      }
      customer.value = null
    }
  } catch (error) {
    if (stale()) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    doc.value = null
    if (isApiError(error) && error.status === 404) {
      notFound.value = true
    } else {
      loadRequestId.value = isApiError(error) ? error.requestId : ''
      loadFailed.value = true
    }
  } finally {
    if (!stale()) {
      loading.value = false
    }
  }
}

onMounted(() => {
  if (!sessionReady.value && sessionFailed.value) {
    // The CSRF token is missing: try the session once more.
    void loadSession()
  }
  void load()
})

// A move to another document (the address changed from outside) reloads.
watch(
  () => route.params.id,
  (id) => {
    if (typeof id === 'string' && id !== '' && id !== doc.value?.id) {
      void load()
    }
  },
)

const title = computed(() => {
  const d = doc.value
  if (!d) {
    return t('documents.detail.title')
  }
  return d.number ?? t(`documents.draftName.${d.type}`)
})

const subject = computed(() => {
  const d = doc.value
  if (!d) {
    return ''
  }
  const name = customer.value?.name
  return name ? `${t(`documents.type.${d.type}`)}, ${name}` : t(`documents.type.${d.type}`)
})

function money(cents: number): string {
  return formatCents(cents, locale.value)
}

function period(d: Doc): string {
  if (d.service_date && d.service_date_to) {
    return `${d.service_date} – ${d.service_date_to}`
  }
  return d.service_date ?? ''
}

function lineDiscount(line: DocumentLine): string {
  if (line.line_discount_percent_permyriad !== null) {
    return `${formatPermyriad(line.line_discount_percent_permyriad, locale.value)} %`
  }
  if (line.line_discount_amount_cents !== null) {
    return money(line.line_discount_amount_cents)
  }
  return ''
}

function documentDiscount(d: Doc): string {
  if (d.discount_percent_permyriad !== null) {
    return `${formatPermyriad(d.discount_percent_permyriad, locale.value)} %`
  }
  if (d.discount_amount_cents !== null) {
    return money(d.discount_amount_cents)
  }
  return ''
}

// The actions of the detail view.
const actionError = ref<IssueFailure | null>(null)
const working = ref(false)
const discardOpen = ref(false)
const discarding = ref(false)

const actionErrorText = computed(() => {
  const f = actionError.value
  return f ? t(`documents.issue.errors.${f.kind}`) : ''
})

/** gate answers false and tells why when a write has no CSRF token yet. */
function gate(): boolean {
  if (sessionReady.value) {
    return true
  }
  notify({ kind: 'error', title: t('common.errors.session') })
  void loadSession()
  return false
}

/** runAction starts a call that answers a new draft and opens its editor. */
async function runAction(call: () => Promise<Doc>): Promise<void> {
  if (!gate()) {
    return
  }
  working.value = true
  actionError.value = null
  try {
    const created = await call()
    if (unmounted.signal.aborted) {
      return
    }
    await router.push({ name: 'document-edit', params: { id: created.id } })
  } catch (error) {
    if (unmounted.signal.aborted) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    actionError.value = classifyIssueError(error)
  } finally {
    if (!unmounted.signal.aborted) {
      working.value = false
    }
  }
}

function correct(): void {
  if (doc.value) {
    void runAction(() => createCorrection((doc.value as Doc).id))
  }
}

function toInvoice(): void {
  if (doc.value) {
    void runAction(() => offerToInvoice((doc.value as Doc).id))
  }
}

async function confirmDiscard(): Promise<void> {
  if (!doc.value || !gate()) {
    return
  }
  discarding.value = true
  try {
    await deleteDocument(doc.value.id)
    if (unmounted.signal.aborted) {
      return
    }
    discardOpen.value = false
    notify({ kind: 'success', title: t('documents.discard.done') })
    await router.push({ name: 'documents' })
  } catch (error) {
    if (unmounted.signal.aborted) {
      return
    }
    discardOpen.value = false
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    if (isApiError(error) && error.status === 409) {
      // Issued in the meantime: the view is stale, not the click wrong.
      notify({ kind: 'error', title: t('documents.discard.conflict') })
      await load()
      return
    }
    notify({ kind: 'error', title: t('documents.discard.error') })
  } finally {
    if (!unmounted.signal.aborted) {
      discarding.value = false
    }
  }
}

function onIssued(issued: Doc): void {
  doc.value = issued
  effectiveId.value = null
}

function openNewCorrection(id: string): void {
  void router.push({ name: 'document-edit', params: { id } })
}
</script>

<template>
  <GovuePage width="shell" :title="title" :lead="t('documents.detail.lead')">
    <div v-if="loading" role="status">
      <span class="sr-only">{{ t('documents.detail.loading') }}</span>
      <GovueSkeleton :lines="5" />
    </div>

    <GovueErrorState
      v-else-if="notFound"
      :title="t('documents.detail.notFoundTitle')"
      :message="t('documents.detail.notFound')"
      :retry-label="t('documents.detail.backToList')"
      @retry="router.push({ name: 'documents' })"
    />

    <GovueErrorState
      v-else-if="loadFailed || !doc"
      :title="t('documents.detail.loadErrorTitle')"
      :message="t('documents.detail.loadError')"
      :request-id="loadRequestId || undefined"
      @retry="load()"
    />

    <template v-else>
      <div class="flex flex-wrap items-center gap-2">
        <GovueBadge kind="neutral">{{ t(`documents.type.${doc.type}`) }}</GovueBadge>
        <GovueBadge :kind="isIssued ? 'success' : 'warning'">
          {{ t(`documents.state.${doc.state}`) }}
        </GovueBadge>
        <GovueBadge v-if="doc.chain_status" :kind="superseded ? 'warning' : 'info'">
          {{ t(`documents.chain.${doc.chain_status}`) }}
        </GovueBadge>
        <GovueBadge v-if="doc.imported" kind="neutral">{{ t('documents.imported') }}</GovueBadge>
      </div>

      <p
        v-if="isDraft"
        role="status"
        class="rounded-md bg-warning-subtle px-3 py-2 text-warning-text"
      >
        {{ t('documents.detail.draftNote') }}
      </p>
      <p
        v-if="superseded"
        role="status"
        data-testid="superseded-note"
        class="rounded-md bg-warning-subtle px-3 py-2 text-warning-text"
      >
        {{ t('documents.detail.supersededNote') }}
        <RouterLink
          v-if="effectiveId && effectiveId !== doc.id"
          :to="{ name: 'document-detail', params: { id: effectiveId } }"
          class="underline"
        >
          {{ t('documents.detail.openEffective') }}
        </RouterLink>
      </p>

      <dl class="grid gap-3 rounded-lg border border-border bg-surface p-4 sm:grid-cols-2">
        <div>
          <dt class="text-sm text-text-muted">{{ t('documents.fields.customer') }}</dt>
          <dd>
            {{ customer?.name ?? '–' }}
            <span v-if="customer?.deleted_at" class="text-text-muted">
              ({{ t('documents.detail.customerDeleted') }})
            </span>
          </dd>
        </div>
        <div>
          <dt class="text-sm text-text-muted">{{ t('documents.fields.language') }}</dt>
          <dd>{{ t(`documents.language.${doc.language}`) }}</dd>
        </div>
        <div>
          <dt class="text-sm text-text-muted">{{ t('documents.fields.regime') }}</dt>
          <dd>{{ t(`documents.regime.${doc.vat_regime}`) }}</dd>
        </div>
        <div v-if="doc.type !== 'offer'">
          <dt class="text-sm text-text-muted">{{ t('documents.fields.servicePeriod') }}</dt>
          <dd>{{ period(doc) || '–' }}</dd>
        </div>
        <div v-if="doc.type !== 'offer'">
          <dt class="text-sm text-text-muted">{{ t('documents.fields.dueDate') }}</dt>
          <dd>{{ doc.due_date ?? '–' }}</dd>
        </div>
        <div v-if="doc.type === 'offer'">
          <dt class="text-sm text-text-muted">{{ t('documents.fields.validUntil') }}</dt>
          <dd>{{ doc.valid_until ?? '–' }}</dd>
        </div>
        <div v-if="doc.customer_reference">
          <dt class="text-sm text-text-muted">{{ t('documents.fields.customerReference') }}</dt>
          <dd>{{ doc.customer_reference }}</dd>
        </div>
        <div v-if="doc.corrects_document_id">
          <dt class="text-sm text-text-muted">{{ t('documents.detail.corrects') }}</dt>
          <dd>
            <RouterLink
              :to="{ name: 'document-detail', params: { id: doc.corrects_document_id } }"
              class="underline"
            >
              {{ t('documents.detail.openOriginal') }}
            </RouterLink>
          </dd>
        </div>
        <div v-if="doc.source_offer_id">
          <dt class="text-sm text-text-muted">{{ t('documents.detail.sourceOffer') }}</dt>
          <dd>
            <RouterLink
              :to="{ name: 'document-detail', params: { id: doc.source_offer_id } }"
              class="underline"
            >
              {{ t('documents.detail.openOffer') }}
            </RouterLink>
          </dd>
        </div>
        <div v-if="doc.payment_terms" class="sm:col-span-2">
          <dt class="text-sm text-text-muted">{{ t('documents.fields.paymentTerms') }}</dt>
          <dd class="whitespace-pre-line">{{ doc.payment_terms }}</dd>
        </div>
        <div v-if="doc.notes" class="sm:col-span-2">
          <dt class="text-sm text-text-muted">{{ t('documents.fields.notes') }}</dt>
          <dd class="whitespace-pre-line">{{ doc.notes }}</dd>
        </div>
      </dl>

      <section aria-labelledby="lines-heading" class="flex flex-col gap-2">
        <h2 id="lines-heading" class="text-xl font-semibold text-text">
          {{ t('documents.detail.lines') }}
        </h2>
        <div class="overflow-x-auto rounded-lg border border-border bg-surface">
          <table class="w-full text-left text-base">
            <caption class="sr-only">
              {{
                t('documents.detail.linesCaption')
              }}
            </caption>
            <thead class="border-b border-border bg-bg-subtle text-sm text-text-muted">
              <tr>
                <th scope="col" class="px-3 py-2 font-medium">#</th>
                <th scope="col" class="px-3 py-2 font-medium">
                  {{ t('documents.lines.description') }}
                </th>
                <th scope="col" class="px-3 py-2 text-right font-medium">
                  {{ t('documents.lines.quantity') }}
                </th>
                <th scope="col" class="px-3 py-2 font-medium">{{ t('documents.lines.unit') }}</th>
                <th scope="col" class="px-3 py-2 text-right font-medium">
                  {{ t('documents.lines.unitPrice') }}
                </th>
                <th scope="col" class="px-3 py-2 text-right font-medium">
                  {{ t('documents.lines.vatRate') }}
                </th>
                <th scope="col" class="px-3 py-2 text-right font-medium">
                  {{ t('documents.lines.discount') }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="line in doc.lines ?? []"
                :key="line.position"
                class="border-b border-border last:border-b-0"
              >
                <td class="px-3 py-2">{{ line.position }}</td>
                <th scope="row" class="px-3 py-2 font-medium whitespace-pre-line">
                  {{ line.description }}
                </th>
                <td class="px-3 py-2 text-right">
                  {{ formatQuantity(line.quantity_thousandths, locale) }}
                </td>
                <td class="px-3 py-2">{{ t(`documents.units.${line.unit}`) }}</td>
                <td class="px-3 py-2 text-right">{{ money(line.unit_price_cents) }}</td>
                <td class="px-3 py-2 text-right">
                  {{ formatPermyriad(line.vat_rate_permyriad, locale) }} %
                </td>
                <td class="px-3 py-2 text-right">{{ lineDiscount(line) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="doc.totals" aria-labelledby="totals-heading" class="flex flex-col gap-2">
        <h2 id="totals-heading" class="text-xl font-semibold text-text">
          {{ t('documents.totals.title') }}
        </h2>
        <dl class="grid max-w-md grid-cols-[1fr_auto] gap-x-6 gap-y-1" data-testid="totals">
          <dt>{{ t('documents.totals.net') }}</dt>
          <dd class="text-right">{{ money(doc.totals.net_total_cents) }}</dd>
          <template v-if="doc.totals.document_discount_cents > 0">
            <dt>{{ t('documents.totals.discount', { discount: documentDiscount(doc) }) }}</dt>
            <dd class="text-right">−{{ money(doc.totals.document_discount_cents) }}</dd>
            <dt>{{ t('documents.totals.netAfterDiscount') }}</dt>
            <dd class="text-right">{{ money(doc.totals.net_after_document_discount_cents) }}</dd>
          </template>
          <template v-if="doc.vat_regime === 'standard'">
            <template v-for="bucket in doc.totals.vat" :key="bucket.rate_permyriad">
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
          <dd class="text-right font-semibold">{{ money(doc.totals.grand_total_cents) }}</dd>
        </dl>
        <p v-if="doc.vat_regime === 'kleinunternehmer'" class="text-sm text-text-muted">
          {{ t('documents.totals.kleinunternehmer') }}
        </p>
      </section>

      <section aria-labelledby="actions-heading" class="flex flex-col gap-3">
        <h2 id="actions-heading" class="text-xl font-semibold text-text">
          {{ t('documents.detail.actions') }}
        </h2>

        <template v-if="isDraft">
          <div class="flex flex-wrap gap-3">
            <GovueButton
              variant="secondary"
              @click="router.push({ name: 'document-edit', params: { id: doc.id } })"
            >
              {{ t('documents.detail.edit') }}
            </GovueButton>
            <GovueButton variant="secondary" :disabled="!sessionReady" @click="discardOpen = true">
              {{ t('documents.discard.action') }}
            </GovueButton>
          </div>
          <DocumentIssue
            :document-id="doc.id"
            :document-type="doc.type"
            :corrects-document-id="doc.corrects_document_id"
            :subject="subject"
            :is-owner="isOwner"
            list-all-problems
            @issued="onIssued"
            @new-correction="openNewCorrection"
          />
        </template>

        <template v-if="doc.type === 'offer'">
          <div>
            <GovueButton
              variant="secondary"
              :disabled="!sessionReady"
              :loading="working"
              :loading-label="t('documents.detail.working')"
              @click="toInvoice"
            >
              {{ t('documents.detail.toInvoice') }}
            </GovueButton>
          </div>
        </template>

        <template v-if="isIssued && isChainDoc">
          <div v-if="isOwner && !superseded">
            <GovueButton
              variant="secondary"
              :disabled="!sessionReady"
              :loading="working"
              :loading-label="t('documents.detail.working')"
              @click="correct"
            >
              {{ t('documents.detail.correct') }}
            </GovueButton>
            <p class="mt-1 text-sm text-text-muted">{{ t('documents.detail.correctHint') }}</p>
          </div>
          <p v-else-if="!isOwner" class="text-sm text-text-muted">
            {{ t('documents.detail.correctOwnerOnly') }}
          </p>
        </template>

        <p v-if="sessionFailed && !sessionReady" role="alert" class="text-danger-text">
          {{ t('common.errors.session') }}
        </p>
        <p v-if="actionError" role="alert" data-testid="action-error" class="text-danger-text">
          {{ actionErrorText }}
        </p>

        <div v-if="isIssued">
          <GovueButton variant="secondary" disabled aria-describedby="pdf-note">
            {{ t('documents.detail.pdf') }}
          </GovueButton>
          <p id="pdf-note" class="mt-1 text-sm text-text-muted">
            {{ t('documents.detail.pdfNote') }}
          </p>
        </div>
      </section>

      <PaymentsPanel
        v-if="isIssued && isChainDoc"
        :document-id="doc.id"
        :is-owner="isOwner"
        @loaded="effectiveId = $event"
      />
    </template>

    <GovueConfirmDialog
      v-model:open="discardOpen"
      :title="t('documents.discard.title')"
      :message="t('documents.discard.message')"
      :subject="subject"
      :confirm-label="t('documents.discard.confirm')"
      :cancel-label="t('documents.cancel')"
      :loading="discarding"
      @confirm="confirmDiscard"
    />
  </GovuePage>
</template>
