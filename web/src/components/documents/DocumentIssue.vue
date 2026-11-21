<script setup lang="ts">
// DocumentIssue is the issue action of a draft: a button, a confirmation that
// names the immutability, and the answer to every way the server can refuse
// (docs/api.md, "POST /api/v1/documents/{id}/issue"). Field errors in
// `details` go to the parent so the editor can mark its fields; anything
// without a field of its own, such as the seller profile, is listed here.
// The browser never forms a number: the issued document comes back whole.
import { GovueButton, GovueConfirmDialog, isApiError, useToasts } from '@ostendorf-cloud/govue'
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRouter } from 'vue-router'

import {
  documentFieldErrors,
  documentProblemList,
  type GeneralProblem,
  type MappedErrors,
} from '../../documentForm'
import {
  classifyIssueError,
  createCorrection,
  deleteDocument,
  issueDocument,
  type Doc,
  type DocumentType,
  type IssueFailure,
  type Totals,
} from '../../documents'
import { formatCents } from '../../money'
import { clearSession, loadSession, sessionFailed, sessionReady } from '../../session'

const props = defineProps<{
  documentId: string
  documentType: DocumentType
  /** The root invoice of a correction draft, to start a new correction. */
  correctsDocumentId: string | null
  /** Names the document in the confirmation. */
  subject: string
  /** Only the owner issues (docs/api.md). */
  isOwner: boolean
  /** Runs before the confirmation opens. The editor saves its draft here and
   *  answers the saved draft, or null when it could not save. The totals the
   *  server stored with it are what the confirmation shows. */
  beforeIssue?: () => Promise<Doc | null>
  /** The totals of the stored draft, for a view that does not save first. */
  totals?: Totals | null
  /** Why issuing is not possible right now, as text. Empty means it is. */
  blockedHint?: string
  /** True in the detail view, which has no fields: every problem is listed. */
  listAllProblems?: boolean
}>()

const emit = defineEmits<{
  issued: [doc: Doc]
  fields: [mapped: MappedErrors]
  newCorrection: [id: string]
  discarded: []
}>()

const i18n = useI18n()
const { t, locale } = i18n
const te = (key: string): boolean => i18n.te(key)
const router = useRouter()
const { notify } = useToasts()

const confirmOpen = ref(false)
const issuing = ref(false)
const preparing = ref(false)
const failure = ref<IssueFailure | null>(null)
const problems = ref<GeneralProblem[]>([])
const recreating = ref(false)
// Which call the failure belongs to: the 503 of a correction says something
// else than the 503 of an issue (no draft is saved then).
const context = ref<'issue' | 'correction'>('issue')
// The totals of the stored draft the confirmation shows.
const confirmTotals = ref<Totals | null>(null)
const discardOpen = ref(false)
const discarding = ref(false)

const unmounted = new AbortController()
onBeforeUnmount(() => unmounted.abort())

async function toLogin(): Promise<void> {
  clearSession()
  await router.push({ name: 'login' })
}

const profileLink = computed(() => router.hasRoute('settings'))

function reset(): void {
  failure.value = null
  problems.value = []
  emit('fields', { fields: {}, general: [] })
}

async function ask(): Promise<void> {
  reset()
  if (props.blockedHint) {
    // The totals on screen are not the settled ones of the draft.
    return
  }
  if (!sessionReady.value) {
    // The CSRF token comes from /me: try the session once more.
    void loadSession()
    return
  }
  confirmTotals.value = props.totals ?? null
  if (props.beforeIssue) {
    preparing.value = true
    const saved = await props.beforeIssue()
    if (unmounted.signal.aborted) {
      return
    }
    preparing.value = false
    if (!saved) {
      return
    }
    confirmTotals.value = saved.totals
  }
  confirmOpen.value = true
}

/** confirmSubject names the document and the total the server stored for it,
 *  so the owner confirms the figure that will be frozen. */
const confirmSubject = computed(() => {
  const totals = confirmTotals.value
  if (!totals) {
    return props.subject
  }
  const money = (cents: number): string => formatCents(cents, locale.value)
  const line = t('documents.issue.confirmTotals', {
    gross: money(totals.grand_total_cents),
    net: money(totals.net_after_document_discount_cents),
    vat: money(totals.vat_total_cents),
  })
  return `${props.subject}: ${line}`
})

function showFailure(error: unknown, from: 'issue' | 'correction' = 'issue'): void {
  const result = classifyIssueError(error)
  context.value = from
  failure.value = result
  if (result.kind === 'fields') {
    const mapped = documentFieldErrors(result.details, t, te)
    emit('fields', mapped)
    problems.value = props.listAllProblems
      ? documentProblemList(result.details, t, te)
      : mapped.general
  }
}

async function confirm(): Promise<void> {
  if (!sessionReady.value) {
    notify({ kind: 'error', title: t('common.errors.session') })
    void loadSession()
    return
  }
  issuing.value = true
  try {
    const issued = await issueDocument(props.documentId)
    if (unmounted.signal.aborted) {
      return
    }
    confirmOpen.value = false
    notify({ kind: 'success', title: t('documents.issue.done', { number: issued.number ?? '' }) })
    emit('issued', issued)
  } catch (error) {
    if (unmounted.signal.aborted) {
      return
    }
    confirmOpen.value = false
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    showFailure(error)
  } finally {
    if (!unmounted.signal.aborted) {
      issuing.value = false
    }
  }
}

/** startNewCorrection creates a fresh correction draft from the chain's new
 *  effective document. The stale draft stays where it is. */
async function startNewCorrection(): Promise<void> {
  if (!props.correctsDocumentId) {
    return
  }
  if (!sessionReady.value) {
    notify({ kind: 'error', title: t('common.errors.session') })
    void loadSession()
    return
  }
  recreating.value = true
  try {
    const created = await createCorrection(props.correctsDocumentId)
    if (unmounted.signal.aborted) {
      return
    }
    emit('newCorrection', created.id)
  } catch (error) {
    if (unmounted.signal.aborted) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    showFailure(error, 'correction')
  } finally {
    if (!unmounted.signal.aborted) {
      recreating.value = false
    }
  }
}

/** confirmDiscard deletes the stale draft after a changed correction base. */
async function confirmDiscard(): Promise<void> {
  if (!sessionReady.value) {
    notify({ kind: 'error', title: t('common.errors.session') })
    void loadSession()
    return
  }
  discarding.value = true
  try {
    await deleteDocument(props.documentId)
    if (unmounted.signal.aborted) {
      return
    }
    discardOpen.value = false
    notify({ kind: 'success', title: t('documents.issue.discarded') })
    emit('discarded')
  } catch (error) {
    if (unmounted.signal.aborted) {
      return
    }
    discardOpen.value = false
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    notify({ kind: 'error', title: t('documents.issue.discardError') })
  } finally {
    if (!unmounted.signal.aborted) {
      discarding.value = false
    }
  }
}

const message = computed(() => {
  const f = failure.value
  if (!f) {
    return ''
  }
  switch (f.kind) {
    case 'regimeMismatch':
      return t('documents.issue.errors.regimeMismatch', {
        regime: f.details.vat_regime
          ? t(`documents.regime.${f.details.vat_regime}`)
          : t('documents.issue.errors.regimeUnknown'),
      })
    case 'baseChanged':
      return t('documents.issue.errors.baseChanged', { seq: f.details.correction_base_seq ?? '' })
    case 'renderer':
      return t(
        context.value === 'correction'
          ? 'documents.issue.errors.rendererCorrection'
          : 'documents.issue.errors.renderer',
      )
    default:
      return t(`documents.issue.errors.${f.kind}`)
  }
})
</script>

<template>
  <div class="flex flex-col gap-3">
    <template v-if="isOwner">
      <p v-if="sessionFailed && !sessionReady" role="alert" class="text-danger-text">
        {{ t('common.errors.session') }}
      </p>
      <div>
        <GovueButton
          variant="primary"
          :disabled="!sessionReady || !!blockedHint"
          :loading="preparing"
          :loading-label="t('documents.issue.preparing')"
          :aria-describedby="blockedHint ? 'issue-blocked' : undefined"
          @click="ask"
        >
          {{ t('documents.issue.action') }}
        </GovueButton>
        <p v-if="blockedHint" id="issue-blocked" class="mt-1 text-sm text-text-muted">
          {{ blockedHint }}
        </p>
      </div>
    </template>
    <p v-else class="text-sm text-text-muted">{{ t('documents.issue.ownerOnly') }}</p>

    <div v-if="failure" role="alert" class="flex flex-col gap-2 text-danger-text">
      <p data-testid="issue-error">{{ message }}</p>
      <ul v-if="problems.length > 0" class="list-disc pl-6">
        <li v-for="p in problems" :key="p.path">
          <span class="font-medium">{{ p.label }}</span>: {{ p.text }}
        </li>
      </ul>
      <p
        v-if="
          failure.kind === 'fields' &&
          profileLink &&
          problems.some((p) => p.path.startsWith('seller.'))
        "
      >
        <RouterLink :to="{ name: 'settings' }" class="underline">
          {{ t('documents.issue.toProfile') }}
        </RouterLink>
      </p>
      <div v-if="failure.kind === 'baseChanged' && correctsDocumentId">
        <GovueButton
          variant="secondary"
          :loading="recreating"
          :loading-label="t('documents.issue.recreating')"
          :disabled="!sessionReady"
          @click="startNewCorrection"
        >
          {{ t('documents.issue.newCorrection') }}
        </GovueButton>
        <GovueButton variant="secondary" :disabled="!sessionReady" @click="discardOpen = true">
          {{ t('documents.issue.discardOld') }}
        </GovueButton>
      </div>
    </div>

    <GovueConfirmDialog
      v-model:open="discardOpen"
      :title="t('documents.issue.discardTitle')"
      :message="t('documents.issue.discardMessage')"
      :subject="subject"
      :confirm-label="t('documents.issue.discardConfirm')"
      :cancel-label="t('documents.cancel')"
      :loading="discarding"
      @confirm="confirmDiscard"
    />

    <GovueConfirmDialog
      v-model:open="confirmOpen"
      :title="t(`documents.issue.confirmTitle.${documentType}`)"
      :message="t('documents.issue.confirmMessage')"
      :subject="confirmSubject"
      :confirm-label="t('documents.issue.confirm')"
      :cancel-label="t('documents.cancel')"
      :loading="issuing"
      @confirm="confirm"
    />
  </div>
</template>
