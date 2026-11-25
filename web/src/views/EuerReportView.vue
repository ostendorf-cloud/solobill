<script setup lang="ts">
// EuerReportView shows the EÜR of one tax year (GET /api/v1/reports/euer/
// {year}): Zeile, Kennzahl and amount per line, each line expandable to the
// payments, expenses, other income and assets behind it. Every figure comes
// from the server; the browser computes none.
//
// The view always shows the disclaimer: the figures are computed from the
// owner's entries and are no tax advice. An unverified mapping and an
// unverified line are marked. The entries of 22 December to 10 January are
// listed, and the owner may assign each one to the other year. Whether the
// 10 day rule applies is not decided here (NOT VERIFIED).
//
// The imported years 2023 to 2025 have no report: the server answers 404 and
// the view says the year is an archive year.
import {
  ErrorCode,
  GovueBadge,
  GovueButton,
  GovueEmptyState,
  GovueErrorState,
  GovueField,
  GovuePage,
  GovueSelect,
  GovueSkeleton,
  isApiError,
  useToasts,
} from '@ostendorf-cloud/govue'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { apiClient } from '../api'
import {
  FIRST_REPORT_YEAR,
  countedYear,
  euerCsvUrl,
  otherYear,
  reportYears,
  type EuerAssignment,
  type EuerBoundaryEntry,
  type EuerLine,
  type EuerReport,
} from '../euerReport'
import { formatCents } from '../money'
import { clearSession, loadSession, me, sessionReady } from '../session'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const { notify } = useToasts()

const years = reportYears(new Date())

/** The year of the path, or the newest offered year. */
const year = computed(() => {
  const raw = route.params.year
  const value = Number(Array.isArray(raw) ? raw[0] : raw)
  return Number.isInteger(value) && value > 0 ? value : years[0]
})
const yearChoice = computed({
  get: () => String(year.value),
  set: (value: string) => {
    void router.replace({ name: 'euer-report', params: { year: value } })
  },
})
const yearOptions = computed(() => {
  const list = years.map((y) => ({ value: String(y), label: String(y) }))
  if (!years.includes(year.value)) {
    list.push({ value: String(year.value), label: String(year.value) })
  }
  return list
})

const report = ref<EuerReport | null>(null)
const assignments = ref<EuerAssignment[]>([])
const loading = ref(true)
type LoadProblem = '' | 'archive' | 'noMapping' | 'regime' | 'failed'
const problem = ref<LoadProblem>('')
const loadRequestId = ref('')
const expanded = ref<Set<string>>(new Set())
const assigning = ref('')
const assignError = ref('')

const isOwner = computed(() => me.value?.role === 'owner')
const unverified = computed(() => report.value?.mapping.status === 'unverified')

const abort = new AbortController()
onBeforeUnmount(() => abort.abort())
let latestRequest = 0

async function toLogin(): Promise<void> {
  clearSession()
  await router.push({ name: 'login' })
}

async function load(showSkeleton = true): Promise<void> {
  const request = ++latestRequest
  const wanted = year.value
  if (showSkeleton) {
    loading.value = true
  }
  problem.value = ''
  try {
    const [answer, list] = await Promise.all([
      apiClient.get<EuerReport>(`/reports/euer/${wanted}`, { signal: abort.signal }),
      apiClient.get<{ assignments: EuerAssignment[] }>('/euer-assignments', {
        signal: abort.signal,
      }),
    ])
    if (abort.signal.aborted || request !== latestRequest) {
      return
    }
    report.value = answer
    assignments.value = list.assignments
  } catch (error) {
    if (abort.signal.aborted || request !== latestRequest) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    report.value = null
    if (isApiError(error) && error.status === 404) {
      problem.value = wanted < FIRST_REPORT_YEAR ? 'archive' : 'noMapping'
    } else if (isApiError(error) && error.code === 'vat_regime_missing') {
      problem.value = 'regime'
    } else {
      loadRequestId.value = isApiError(error) ? error.requestId : ''
      problem.value = 'failed'
    }
  } finally {
    if (!abort.signal.aborted && request === latestRequest) {
      loading.value = false
    }
  }
}

watch(
  year,
  () => {
    expanded.value = new Set()
    void load()
  },
  { immediate: true },
)

function label(line: EuerLine): string {
  return locale.value.startsWith('de') ? line.label_de : line.label_en
}

/** The operator is printed the way the form prints it, with a real minus. */
function operatorText(op: string): string {
  return op === '-' ? '−' : op
}

function toggle(key: string): void {
  const next = new Set(expanded.value)
  if (next.has(key)) {
    next.delete(key)
  } else {
    next.add(key)
  }
  expanded.value = next
}

function effectiveAssignment(entry: EuerBoundaryEntry): EuerAssignment | undefined {
  return assignments.value.find((a) => a.entry_kind === entry.kind && a.entry_id === entry.id)
}

/** The year the owner may move the entry to: the other one of its date's
 *  year and the neighbouring year. */
function targetYear(entry: EuerBoundaryEntry): number {
  const dateYear = Number(entry.date.slice(0, 4))
  return countedYear(entry) === dateYear ? otherYear(entry.date) : dateYear
}

async function assign(entry: EuerBoundaryEntry): Promise<void> {
  assignError.value = ''
  if (!sessionReady.value) {
    assignError.value = t('common.errors.session')
    void loadSession()
    return
  }
  const target = targetYear(entry)
  assigning.value = entry.id
  try {
    await apiClient.post(
      '/euer-assignments',
      {
        entry_kind: entry.kind,
        entry_id: entry.id,
        tax_year: target,
        supersedes_id: effectiveAssignment(entry)?.id ?? null,
      },
      { signal: abort.signal },
    )
    if (abort.signal.aborted) {
      return
    }
    notify({ kind: 'success', title: t('reports.boundary.assigned', { year: target }) })
    await load(false)
  } catch (error) {
    if (abort.signal.aborted) {
      return
    }
    if (isApiError(error) && error.isUnauthenticated) {
      await toLogin()
      return
    }
    if (isApiError(error) && error.code === ErrorCode.conflict) {
      assignError.value = t('reports.boundary.conflict')
      await load(false)
    } else if (isApiError(error) && error.status === 403) {
      assignError.value = t('common.errors.forbidden')
    } else {
      assignError.value = t('common.errors.generic')
    }
  } finally {
    if (!abort.signal.aborted) {
      assigning.value = ''
    }
  }
}
</script>

<template>
  <GovuePage width="shell" :title="t('reports.title', { year })" :lead="t('reports.lead')">
    <p
      role="note"
      data-testid="euer-disclaimer"
      class="rounded-lg border border-border bg-bg-subtle px-4 py-3 text-base"
    >
      {{ t('reports.disclaimer') }}
    </p>

    <form class="flex flex-wrap items-end gap-4" @submit.prevent>
      <GovueField :label="t('reports.year')">
        <GovueSelect v-model="yearChoice" :options="yearOptions" />
      </GovueField>
      <a
        v-if="report"
        :href="euerCsvUrl(report.year)"
        download
        class="text-accent underline underline-offset-2"
      >
        {{ t('reports.csv', { year: report.year }) }}
      </a>
    </form>

    <div v-if="loading" role="status">
      <span class="sr-only">{{ t('reports.loading') }}</span>
      <GovueSkeleton :lines="6" />
    </div>

    <GovueEmptyState
      v-else-if="problem === 'archive'"
      :title="t('reports.archiveTitle', { year })"
      :description="t('reports.archiveDescription')"
    />
    <GovueEmptyState
      v-else-if="problem === 'noMapping'"
      :title="t('reports.noMappingTitle', { year })"
      :description="t('reports.noMappingDescription')"
    />
    <GovueErrorState
      v-else-if="problem === 'regime'"
      :title="t('reports.loadErrorTitle')"
      :message="t('reports.regimeMissing')"
      @retry="load()"
    />
    <GovueErrorState
      v-else-if="problem === 'failed'"
      :title="t('reports.loadErrorTitle')"
      :message="t('reports.loadError')"
      :request-id="loadRequestId || undefined"
      @retry="load()"
    />

    <template v-else-if="report">
      <section class="flex flex-col gap-2" :aria-label="t('reports.mapping.heading')">
        <p class="flex flex-wrap items-center gap-2">
          <span>{{ t('reports.mapping.label', { year: report.mapping.tax_year }) }}</span>
          <GovueBadge
            data-testid="mapping-status"
            :kind="report.mapping.status === 'verified' ? 'success' : 'warning'"
          >
            {{ t(`reports.mapping.${report.mapping.status}`) }}
          </GovueBadge>
        </p>
        <p
          v-if="unverified"
          role="note"
          data-testid="unverified-mapping"
          class="rounded-lg border border-border bg-warning-subtle px-4 py-3 text-base text-warning-text"
        >
          {{ t('reports.mapping.unverifiedNote', { year: report.mapping.tax_year }) }}
        </p>
        <p class="text-text-muted">
          {{ t('reports.regimes') }}
          <span v-for="(p, i) in report.vat_regimes" :key="p.valid_from">
            {{ i > 0 ? ', ' : ''
            }}{{
              t('reports.regimeSince', {
                regime: t(`reports.regime.${p.regime}`),
                date: p.valid_from,
              })
            }}
          </span>
        </p>
      </section>

      <GovueEmptyState
        v-if="report.lines.length === 0"
        :title="t('reports.emptyTitle')"
        :description="t('reports.emptyDescription')"
      />

      <div v-else class="overflow-x-auto rounded-lg border border-border bg-surface">
        <table class="w-full text-left text-base">
          <caption class="sr-only">
            {{
              t('reports.tableCaption', { year: report.year })
            }}
          </caption>
          <thead class="border-b border-border bg-bg-subtle text-sm text-text-muted">
            <tr>
              <th scope="col" class="px-3 py-2 font-medium">{{ t('reports.columns.zeile') }}</th>
              <th scope="col" class="px-3 py-2 font-medium">
                {{ t('reports.columns.kennzahl') }}
              </th>
              <th scope="col" class="px-3 py-2 font-medium">{{ t('reports.columns.label') }}</th>
              <th scope="col" class="px-3 py-2 text-right font-medium">
                {{ t('reports.columns.amount') }}
              </th>
              <th scope="col" class="px-3 py-2 font-medium">
                {{ t('reports.columns.sources') }}
              </th>
            </tr>
          </thead>
          <tbody>
            <template v-for="line in report.lines" :key="line.key">
              <tr class="border-b border-border" :data-testid="`line-${line.key}`">
                <td class="px-3 py-2">{{ line.zeile }}</td>
                <td class="px-3 py-2">
                  <span v-if="line.kennzahl">
                    {{ line.operator ? `${operatorText(line.operator)} ` : '' }}{{ line.kennzahl }}
                  </span>
                  <span v-else class="text-text-muted">{{ t('reports.noKennzahl') }}</span>
                </td>
                <th scope="row" class="px-3 py-2 font-medium">
                  {{ label(line) }}
                  <GovueBadge v-if="!line.verified" kind="warning" class="ml-2">
                    {{ t('reports.lineUnverified') }}
                  </GovueBadge>
                </th>
                <td class="px-3 py-2 text-right">{{ formatCents(line.amount_cents, locale) }}</td>
                <td class="px-3 py-2">
                  <GovueButton
                    variant="quiet"
                    :aria-expanded="expanded.has(line.key)"
                    :aria-controls="`sources-${line.key}`"
                    :aria-label="
                      t('reports.toggleSources', { count: line.sources.length, zeile: line.zeile })
                    "
                    @click="toggle(line.key)"
                  >
                    {{
                      expanded.has(line.key)
                        ? t('reports.hideSources')
                        : t('reports.showSources', { count: line.sources.length })
                    }}
                  </GovueButton>
                </td>
              </tr>
              <tr
                v-if="expanded.has(line.key)"
                :id="`sources-${line.key}`"
                class="border-b border-border bg-bg-subtle"
              >
                <td colspan="5" class="px-3 py-2">
                  <ul class="flex flex-col gap-1 text-sm">
                    <li
                      v-for="s in line.sources"
                      :key="`${s.kind}-${s.id}`"
                      class="flex flex-wrap gap-x-4"
                    >
                      <span>{{ s.date }}</span>
                      <span>{{ t(`reports.kind.${s.kind}`) }}</span>
                      <span class="font-mono text-text-muted">{{ s.id }}</span>
                      <span class="ml-auto">{{ formatCents(s.amount_cents, locale) }}</span>
                    </li>
                  </ul>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>

      <section class="flex flex-col gap-2" aria-labelledby="euer-boundary-heading">
        <h2 id="euer-boundary-heading" class="text-lg font-semibold">
          {{ t('reports.boundary.heading') }}
        </h2>
        <p class="text-text-muted">{{ t('reports.boundary.lead') }}</p>
        <p v-if="assignError" role="alert" class="text-danger-text">{{ assignError }}</p>
        <p v-if="report.boundary_entries.length === 0" class="text-text-muted">
          {{ t('reports.boundary.empty') }}
        </p>
        <ul v-else class="flex flex-col gap-2">
          <li
            v-for="entry in report.boundary_entries"
            :key="`${entry.kind}-${entry.id}`"
            class="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg border border-border px-3 py-2"
            :data-testid="`boundary-${entry.id}`"
          >
            <span>{{ entry.date }}</span>
            <span>{{ t(`reports.kind.${entry.kind}`) }}</span>
            <span>{{ formatCents(entry.amount_cents, locale) }}</span>
            <GovueBadge :kind="entry.assigned_tax_year === null ? 'warning' : 'info'">
              {{
                entry.assigned_tax_year === null
                  ? t('reports.boundary.unassigned', { year: countedYear(entry) })
                  : t('reports.boundary.assignedTo', { year: entry.assigned_tax_year })
              }}
            </GovueBadge>
            <GovueButton
              v-if="isOwner"
              variant="secondary"
              :disabled="assigning !== ''"
              @click="assign(entry)"
            >
              {{ t('reports.boundary.assign', { year: targetYear(entry) }) }}
            </GovueButton>
          </li>
        </ul>
      </section>

      <section class="flex flex-col gap-2" aria-labelledby="euer-warnings-heading">
        <h2 id="euer-warnings-heading" class="text-lg font-semibold">
          {{ t('reports.warnings.heading') }}
        </h2>
        <p v-if="report.warnings.length === 0" class="text-text-muted">
          {{ t('reports.warnings.empty') }}
        </p>
        <ul v-else class="flex flex-col gap-1">
          <li
            v-for="w in report.warnings"
            :key="`${w.code}-${w.id}`"
            :data-testid="`warning-${w.code}`"
          >
            {{ t(`reports.warnings.${w.code}`, { count: w.related_ids.length }) }}
            <span class="font-mono text-sm text-text-muted">{{ w.id }}</span>
          </li>
        </ul>
      </section>
    </template>
  </GovuePage>
</template>
