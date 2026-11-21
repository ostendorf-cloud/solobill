import { createGovueI18n, ApiError } from '@ostendorf-cloud/govue'
import { render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../../api'
import type { Doc } from '../../documents'
import { de, en } from '../../i18n/locales'
import { clearSession, me } from '../../session'
import DocumentIssue from './DocumentIssue.vue'

vi.mock('../../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const saved = { id: 'd-1', totals: null } as unknown as Doc

function renderIssue(props: Record<string, unknown>) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  })
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en, de } })
  return render(DocumentIssue, {
    props: {
      documentId: 'd-1',
      documentType: 'invoice',
      correctsDocumentId: null,
      subject: 'Invoice, Customer Ltd',
      isOwner: true,
      ...props,
    },
    global: { plugins: [router, i18n, createGovueI18n({ i18n })] },
  })
}

describe('DocumentIssue', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
  })

  it('does not open the confirmation when the form changed while the draft was saved', async () => {
    let answer: (doc: Doc | null) => void = () => {}
    const beforeIssue = vi.fn(
      () =>
        new Promise<Doc | null>((resolve) => {
          answer = resolve
        }),
    )
    const user = userEvent.setup()
    const { rerender, emitted } = renderIssue({ beforeIssue, blockedHint: '' })

    await user.click(screen.getByRole('button', { name: 'Issue' }))
    await vi.waitFor(() => expect(beforeIssue).toHaveBeenCalledTimes(1))
    expect(emitted().preparing).toEqual([[true]])
    // An edit during the save makes the totals unsettled again.
    await rerender({ blockedHint: 'Issuing waits until the totals are calculated.' })
    answer(saved)

    expect(await screen.findByTestId('issue-notice')).toHaveTextContent(
      'changed while it was being saved',
    )
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(emitted().preparing).toEqual([[true], [false]])
    expect(apiClient.post).not.toHaveBeenCalled()
  })

  it('opens the confirmation when nothing changed during the save', async () => {
    const beforeIssue = vi.fn(() => Promise.resolve(saved))
    const user = userEvent.setup()
    renderIssue({ beforeIssue, blockedHint: '' })

    await user.click(screen.getByRole('button', { name: 'Issue' }))

    expect(await screen.findByRole('dialog')).toBeVisible()
    expect(screen.queryByTestId('issue-notice')).toBeNull()
  })

  describe('the discard of the old draft', () => {
    /** failed renders the panel and lets the issue answer with a failure. */
    async function failed(error: ApiError, corrects: string | null) {
      vi.mocked(apiClient.post).mockRejectedValue(error)
      const user = userEvent.setup()
      renderIssue({ correctsDocumentId: corrects, totals: null })
      await user.click(screen.getByRole('button', { name: 'Issue' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Issue permanently' }))
      await screen.findByTestId('issue-error')
    }

    it('is offered after a changed correction base of a correction', async () => {
      await failed(
        new ApiError({
          code: 'correction_base_changed',
          message: 'x',
          status: 409,
          details: { correction_base_seq: '2' },
        }),
        'd-0',
      )

      expect(screen.getByRole('button', { name: 'Discard the old draft' })).toBeVisible()
      expect(
        screen.getByRole('button', { name: 'Start a new correction from the effective document' }),
      ).toBeVisible()
    })

    it('is not offered when the draft is no correction of a chain', async () => {
      await failed(
        new ApiError({
          code: 'correction_base_changed',
          message: 'x',
          status: 409,
          details: { correction_base_seq: '2' },
        }),
        null,
      )

      expect(screen.queryByRole('button', { name: 'Discard the old draft' })).toBeNull()
      expect(screen.queryByRole('button', { name: /Start a new correction/ })).toBeNull()
    })

    it.each([
      ['renderer', 503, 'renderer_unavailable', {}],
      ['regime mismatch', 409, 'vat_regime_mismatch', { vat_regime: 'standard' }],
      ['regime missing', 400, 'vat_regime_missing', {}],
      ['regime changed', 409, 'correction_regime_changed', {}],
      ['conflict', 409, 'conflict', {}],
      ['field errors', 400, 'invalid_argument', { notes: 'required' }],
      ['forbidden', 403, 'permission_denied', {}],
      ['server', 500, 'internal', {}],
    ])('is not offered after %s', async (_name, status, code, details) => {
      await failed(new ApiError({ code, message: 'x', status, details }), 'd-0')

      expect(screen.queryByRole('button', { name: 'Discard the old draft' })).toBeNull()
      expect(screen.queryByRole('button', { name: /Start a new correction/ })).toBeNull()
    })
  })
})
