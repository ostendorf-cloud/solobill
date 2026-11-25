import { ApiError, ErrorCode, createGovueI18n, useToasts } from '@ostendorf-cloud/govue'
import { render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../api'
import type { EuerAssignment, EuerReport } from '../euerReport'
import { de, en } from '../i18n/locales'
import { clearSession, me } from '../session'
import EuerReportView from './EuerReportView.vue'

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

function report(over: Partial<EuerReport> = {}): EuerReport {
  return {
    year: 2026,
    mapping: { tax_year: 2026, status: 'unverified', source: 'copy of 2025' },
    vat_regimes: [{ valid_from: '2025-01-01', regime: 'kleinunternehmer' }],
    lines: [
      {
        key: 'euer.z12',
        zeile: 12,
        kennzahl: '111',
        operator: '',
        verified: false,
        label_de: 'Betriebseinnahmen als Kleinunternehmer',
        label_en: 'Revenue as a small business',
        amount_cents: 23750,
        sources: [
          { kind: 'payment', id: 'pay-1', date: '2026-01-05', amount_cents: 10000 },
          { kind: 'payment', id: 'pay-2', date: '2026-03-10', amount_cents: 13750 },
        ],
      },
      {
        key: 'euer.z72',
        zeile: 72,
        kennzahl: '142',
        operator: '-',
        verified: true,
        label_de: 'Fahrtkosten Wohnung und erste Betriebsstätte',
        label_en: 'Commuting, deducted from car costs',
        amount_cents: 120000,
        sources: [{ kind: 'expense', id: 'exp-1', date: '2026-12-31', amount_cents: 120000 }],
      },
    ],
    boundary_entries: [
      {
        kind: 'payment',
        id: 'pay-1',
        date: '2026-01-05',
        amount_cents: 10000,
        assigned_tax_year: null,
      },
      {
        kind: 'expense',
        id: 'exp-2',
        date: '2026-12-28',
        amount_cents: 5000,
        assigned_tax_year: 2027,
      },
    ],
    warnings: [
      { code: 'missing_receipt', kind: 'expense', id: 'exp-3', related_ids: ['exp-4'] },
      { code: 'unassigned_boundary_entry', kind: 'payment', id: 'pay-1', related_ids: [] },
    ],
    disclaimer: 'Computed from your entries. No tax advice.',
    ...over,
  }
}

const decemberAssignment: EuerAssignment = {
  id: 'asg-1',
  entry_kind: 'expense',
  entry_id: 'exp-2',
  tax_year: 2027,
  note: null,
  supersedes_id: null,
  created_at: '2026-12-01T09:00:00Z',
}

function apiError(status: number, code: string) {
  return new ApiError({ code, message: 'failed', status })
}

function mockApi(answer: EuerReport | ApiError = report(), assignments = [decemberAssignment]) {
  vi.mocked(apiClient.get).mockImplementation((path: string) => {
    if (path.startsWith('/reports/euer/')) {
      return answer instanceof ApiError ? Promise.reject(answer) : Promise.resolve(answer)
    }
    if (path === '/euer-assignments') {
      return Promise.resolve({ assignments })
    }
    return Promise.reject(apiError(404, ErrorCode.notFound))
  })
}

async function renderView(locale: 'en' | 'de' = 'en', path = '/reports/euer/2026') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/reports/euer/:year(\\d{4})?', name: 'euer-report', component: EuerReportView },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  await router.isReady()
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  const view = render(EuerReportView, { global: { plugins: [router, i18n, govueI18n] } })
  return { ...view, router }
}

describe('EuerReportView', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    useToasts().dismissAll()
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
  })

  it('shows Zeile, Kennzahl with the operator, label and amount per line', async () => {
    mockApi()
    await renderView()

    const table = await screen.findByRole('table', { name: 'EÜR 2026, line by line' })
    const z12 = within(table).getByTestId('line-euer.z12')
    expect(within(z12).getByText('12')).toBeVisible()
    expect(within(z12).getByText('111')).toBeVisible()
    expect(
      within(z12).getByRole('rowheader', { name: /Revenue as a small business/ }),
    ).toBeVisible()
    expect(within(z12).getByText('237.50 EUR')).toBeVisible()
    expect(within(z12).getByText('not verified')).toBeVisible()

    const z72 = within(table).getByTestId('line-euer.z72')
    expect(within(z72).getByText('− 142')).toBeVisible()
    expect(within(z72).getByText('1,200.00 EUR')).toBeVisible()
    expect(within(z72).queryByText('not verified')).toBeNull()
  })

  it('expands a line to the entries behind it', async () => {
    const user = userEvent.setup()
    mockApi()
    await renderView()

    const toggle = await screen.findByRole('button', { name: 'Entries behind Zeile 12 (2)' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('pay-2')).toBeNull()

    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    const sources = document.getElementById('sources-euer.z12')
    expect(sources).not.toBeNull()
    const items = within(sources as HTMLElement).getAllByRole('listitem')
    expect(items).toHaveLength(2)
    expect(within(items[1]).getByText('pay-2')).toBeVisible()
    expect(within(items[1]).getByText('Payment')).toBeVisible()
    expect(within(items[1]).getByText('137.50 EUR')).toBeVisible()

    await user.click(toggle)
    expect(document.getElementById('sources-euer.z12')).toBeNull()
  })

  it.each([
    [
      'en',
      'Computed from your entries. This is no tax advice. Check every figure before you file it.',
    ],
    [
      'de',
      'Berechnet aus Ihren Buchungen. Das ist keine Steuerberatung. Prüfen Sie jeden Wert, bevor Sie ihn übernehmen.',
    ],
  ] as const)('always shows the disclaimer in %s', async (locale, text) => {
    mockApi(apiError(500, 'internal'))
    await renderView(locale)

    const note = await screen.findByTestId('euer-disclaimer')
    expect(note.textContent?.trim()).toBe(text)
  })

  it('marks an unverified mapping with its status and a note', async () => {
    mockApi()
    await renderView()

    expect(await screen.findByTestId('mapping-status')).toHaveTextContent('not verified')
    expect(screen.getByTestId('unverified-mapping')).toHaveTextContent(
      'The line mapping of 2026 is a copy of 2025',
    )
  })

  it('shows no unverified note for a verified mapping', async () => {
    mockApi(report({ mapping: { tax_year: 2026, status: 'verified', source: 'BMF' } }))
    await renderView()

    expect(await screen.findByTestId('mapping-status')).toHaveTextContent('verified')
    expect(screen.queryByTestId('unverified-mapping')).toBeNull()
  })

  it('shows the German strings, labels and number format', async () => {
    mockApi()
    await renderView('de')

    expect(await screen.findByRole('heading', { name: 'EÜR 2026' })).toBeVisible()
    expect(
      screen.getByRole('rowheader', { name: /Betriebseinnahmen als Kleinunternehmer/ }),
    ).toBeVisible()
    expect(screen.getByText('237,50 EUR')).toBeVisible()
    expect(screen.getByTestId('mapping-status')).toHaveTextContent('nicht geprüft')
  })

  it('offers the CSV of the year', async () => {
    mockApi()
    await renderView()

    const link = await screen.findByRole('link', { name: 'Download CSV for 2026' })
    expect(link).toHaveAttribute('href', '/api/v1/reports/euer/2026/csv')
  })

  it('shows an archive year as such, with no report', async () => {
    mockApi(apiError(404, ErrorCode.notFound))
    await renderView('en', '/reports/euer/2025')

    expect(await screen.findByText('No report for 2025')).toBeVisible()
    expect(screen.getByText(/imported archive years/)).toBeVisible()
    expect(screen.queryByRole('table')).toBeNull()
    expect(screen.queryByRole('link', { name: /CSV/ })).toBeNull()
  })

  it('names a missing VAT regime period', async () => {
    mockApi(apiError(400, 'vat_regime_missing'))
    await renderView()

    expect(await screen.findByText(/No VAT regime period covers this year/)).toBeVisible()
  })

  it('lists the warnings with the bundled reversals', async () => {
    mockApi()
    await renderView()

    expect(await screen.findByTestId('warning-missing_receipt')).toHaveTextContent(
      'Expense without a receipt (with 1 reversals) exp-3',
    )
    expect(screen.getByTestId('warning-unassigned_boundary_entry')).toBeVisible()
  })

  it('lets the owner move an unassigned entry to the other year', async () => {
    const user = userEvent.setup()
    mockApi()
    vi.mocked(apiClient.post).mockResolvedValue({})
    await renderView()

    const entry = await screen.findByTestId('boundary-pay-1')
    expect(within(entry).getByText('Counts in 2026, no decision')).toBeVisible()
    await user.click(within(entry).getByRole('button', { name: 'Count in 2025' }))

    expect(apiClient.post).toHaveBeenCalledWith(
      '/euer-assignments',
      { entry_kind: 'payment', entry_id: 'pay-1', tax_year: 2025, supersedes_id: null },
      expect.anything(),
    )
    await vi.waitFor(() => expect(apiClient.get).toHaveBeenCalledTimes(4))
  })

  it('names the effective decision when the owner changes it', async () => {
    const user = userEvent.setup()
    mockApi()
    vi.mocked(apiClient.post).mockResolvedValue({})
    await renderView()

    const entry = await screen.findByTestId('boundary-exp-2')
    expect(within(entry).getByText('Assigned to 2027')).toBeVisible()
    await user.click(within(entry).getByRole('button', { name: 'Count in 2026' }))

    expect(apiClient.post).toHaveBeenCalledWith(
      '/euer-assignments',
      { entry_kind: 'expense', entry_id: 'exp-2', tax_year: 2026, supersedes_id: 'asg-1' },
      expect.anything(),
    )
  })

  it('reloads after a conflicting decision and says so', async () => {
    const user = userEvent.setup()
    mockApi()
    vi.mocked(apiClient.post).mockRejectedValue(apiError(409, ErrorCode.conflict))
    await renderView()

    const entry = await screen.findByTestId('boundary-pay-1')
    await user.click(within(entry).getByRole('button', { name: 'Count in 2025' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('changed in the meantime')
    expect(apiClient.get).toHaveBeenCalledTimes(4)
  })

  it('offers no decision to a member', async () => {
    me.value = { email: 'm@b.test', name: 'Max', role: 'member', csrf_token: 'csrf' }
    mockApi()
    await renderView()

    const entry = await screen.findByTestId('boundary-pay-1')
    expect(within(entry).queryByRole('button')).toBeNull()
  })

  it('goes to the login on 401', async () => {
    mockApi(apiError(401, ErrorCode.unauthenticated))
    const { router } = await renderView()

    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
  })
})
