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
  issueDocument,
  type Doc,
  type DocumentType,
  type IssueFailure,
} from '../../documents'
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
   *  answers false when it could not. */
  beforeIssue?: () => Promise<boolean>
  /** True in the detail view, which has no fields: every problem is listed. */
  listAllProblems?: boolean
}>()

const emit = defineEmits<{
  issued: [doc: Doc]
  fields: [mapped: MappedErrors]
  newCorrection: [id: string]
}>()

const { t } = useI18n()
const router = useRouter()
const { notify } = useToasts()

const confirmOpen = ref(false)
const issuing = ref(false)
const preparing = ref(false)
const failure = ref<IssueFailure | null>(null)
const problems = ref<GeneralProblem[]>([])
const recreating = ref(false)

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
  if (!sessionReady.value) {
    // The CSRF token comes from /me: try the session once more.
    void loadSession()
    return
  }
  if (props.beforeIssue) {
    preparing.value = true
    const ok = await props.beforeIssue()
    if (unmounted.signal.aborted) {
      return
    }
    preparing.value = false
    if (!ok) {
      return
    }
  }
  confirmOpen.value = true
}

function showFailure(error: unknown): void {
  const result = classifyIssueError(error)
  failure.value = result
  if (result.kind === 'fields') {
    const mapped = documentFieldErrors(result.details, t)
    emit('fields', mapped)
    problems.value = props.listAllProblems ? documentProblemList(result.details, t) : mapped.general
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
    showFailure(error)
  } finally {
    if (!unmounted.signal.aborted) {
      recreating.value = false
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
          :disabled="!sessionReady"
          :loading="preparing"
          :loading-label="t('documents.issue.preparing')"
          @click="ask"
        >
          {{ t('documents.issue.action') }}
        </GovueButton>
      </div>
    </template>
    <p v-else class="text-sm text-text-muted">{{ t('documents.issue.ownerOnly') }}</p>

    <div v-if="failure" role="alert" class="flex flex-col gap-2 text-danger-text">
      <p data-testid="issue-error">{{ message }}</p>
      <ul v-if="problems.length > 0" class="list-disc pl-6">
        <li v-for="p in problems" :key="p.path">
          <code class="font-mono text-sm">{{ p.path }}</code>: {{ p.text }}
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
      </div>
    </div>

    <GovueConfirmDialog
      v-model:open="confirmOpen"
      :title="t(`documents.issue.confirmTitle.${documentType}`)"
      :message="t('documents.issue.confirmMessage')"
      :subject="subject"
      :confirm-label="t('documents.issue.confirm')"
      :cancel-label="t('documents.cancel')"
      :loading="issuing"
      @confirm="confirm"
    />
  </div>
</template>
