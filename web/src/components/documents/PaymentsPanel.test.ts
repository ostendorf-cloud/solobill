import { ApiError, ErrorCode, createGovueI18n, useToasts } from '@ostendorf-cloud/govue'
import { render, screen, within } from '@testing-library/vue'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'

import { apiClient } from '../../api'
import type { Payment, PaymentsAnswer } from '../../documents'
import { berlinToday } from '../../documents'
import { de, en } from '../../i18n/locales'
import { clearSession, me, sessionFailed } from '../../session'
import PaymentsPanel from './PaymentsPanel.vue'

vi.mock('../../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

function payment(overrides: Partial<Payment>): Payment {
  return {
    id: 'p-1',
    document_id: 'd-1',
    received_on: '2026-12-03',
    amount_cents: 10000,
    method: 'bank_transfer',
    note: null,
    reverses_payment_id: null,
    imported: false,
    created_at: '2026-12-03T09:00:00Z',
    ...overrides,
  }
}

function answer(payments: Payment[], paid: number, open: number): PaymentsAnswer {
  return { payments, paid_cents: paid, effective_document_id: 'd-2', open_cents: open }
}

// Posts are recorded here and asserted after the fact: an assertion inside
// the mock would be swallowed by the code under test.
let posts: { path: string; body: Record<string, unknown> }[] = []

function mockApi(initial: PaymentsAnswer, afterPost?: PaymentsAnswer) {
  let current = initial
  vi.mocked(apiClient.get).mockImplementation(() => Promise.resolve(current))
  vi.mocked(apiClient.post).mockImplementation((path: string, body?: unknown) => {
    posts.push({ path, body: body as Record<string, unknown> })
    if (afterPost) {
      current = afterPost
    }
    return Promise.resolve({})
  })
}

function renderPanel(props: { documentId?: string; isOwner?: boolean } = {}, locale = 'en') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: { template: '<div />' } },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = createGovueI18n({ i18n })
  const view = render(PaymentsPanel, {
    props: { documentId: 'd-1', isOwner: true, ...props },
    global: { plugins: [router, i18n, govueI18n] },
  })
  return { ...view, router }
}

function apiError(status: number, code: string, details?: Record<string, string>) {
  return new ApiError({ code, message: 'failed', status, details })
}

function toastTitles(): string[] {
  return useToasts().toasts.value.map((toast) => toast.title)
}

const paid = payment({ id: 'p-1', amount_cents: 10000 })
const overpaid = answer([paid, payment({ id: 'p-2', amount_cents: 5000 })], 15000, -2500)

describe('PaymentsPanel', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    posts = []
    useToasts().dismissAll()
    clearSession()
    me.value = { email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' }
  })

  it('lists the payments with the paid and open amounts and names the effective document', async () => {
    mockApi(answer([paid], 10000, 13750))
    const { emitted } = renderPanel()

    const table = await screen.findByRole('table', { name: 'Payments of this invoice' })
    expect(within(table).getByText('2026-12-03')).toBeVisible()
    expect(within(table).getByText('Bank transfer')).toBeVisible()
    expect(within(table).getByText('100.00 EUR')).toBeVisible()
    expect(screen.getByTestId('paid')).toHaveTextContent('100.00 EUR')
    expect(screen.getByTestId('open')).toHaveTextContent('137.50 EUR')
    expect(screen.getByText('Open amount')).toBeVisible()
    expect(screen.queryByText('Refund due')).toBeNull()
    expect(emitted().loaded).toEqual([['d-2']])
    expect(apiClient.get).toHaveBeenCalledWith(
      '/documents/d-1/payments',
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
  })

  it('shows the German strings and number format', async () => {
    mockApi(answer([paid], 10000, 13750))
    renderPanel({}, 'de')

    expect(await screen.findByRole('heading', { name: 'Zahlungen' })).toBeVisible()
    expect(await screen.findByTestId('open')).toHaveTextContent('137,50 EUR')
    expect(screen.getByRole('button', { name: 'Zahlung erfassen' })).toBeVisible()
  })

  it('says that a refund is due when the open amount is negative, with the sign kept in the list', async () => {
    mockApi(
      answer(
        [paid, payment({ id: 'p-2', amount_cents: -2500, received_on: '2026-12-10' })],
        7500,
        -2500,
      ),
    )
    renderPanel()

    expect(await screen.findByText('Refund due')).toBeVisible()
    expect(screen.getByTestId('open')).toHaveTextContent('25.00 EUR')
    expect(screen.getByText(/A refund of 25.00 EUR is due/)).toBeVisible()
    const table = screen.getByRole('table', { name: 'Payments of this invoice' })
    expect(within(table).getByText('-25.00 EUR')).toBeVisible()
    expect(within(table).getByText('Refund')).toBeVisible()
  })

  it('says the invoice is paid in full when nothing is open', async () => {
    mockApi(answer([paid], 10000, 0))
    renderPanel()

    expect(await screen.findByText('The invoice is paid in full.')).toBeVisible()
  })

  it('keeps the refund button off while no refund is due', async () => {
    mockApi(answer([paid], 10000, 3750))
    renderPanel()

    expect(await screen.findByRole('button', { name: 'Record refund' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Record payment' })).toBeEnabled()
  })

  it('records a payment with the typed amount in cents and the CSRF protected post', async () => {
    mockApi(answer([], 0, 23750), answer([paid], 23750, 0))
    const user = userEvent.setup()
    renderPanel()

    await user.click(await screen.findByRole('button', { name: 'Record payment' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/^Amount/), '237,50')
    await user.type(within(dialog).getByLabelText('Note'), 'Thanks')
    await user.click(within(dialog).getByRole('button', { name: 'Record payment' }))

    await vi.waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0]).toEqual({
      path: '/documents/d-1/payments',
      body: {
        // The day is prefilled with today in Berlin.
        received_on: berlinToday(),
        amount_cents: 23750,
        method: 'bank_transfer',
        note: 'Thanks',
        reverses_payment_id: null,
      },
    })
    expect(await screen.findByText('The invoice is paid in full.')).toBeVisible()
    expect(toastTitles()).toContain('The payment was recorded.')
  })

  it('prefills a refund with the refund due and sends it negated', async () => {
    mockApi(overpaid, answer([paid], 10000, 0))
    const user = userEvent.setup()
    renderPanel()

    await user.click(await screen.findByRole('button', { name: 'Record refund' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText(/^Amount/)).toHaveValue('25.00')
    await user.click(within(dialog).getByRole('button', { name: 'Record refund' }))

    await vi.waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0]?.body).toMatchObject({ amount_cents: -2500, reverses_payment_id: null })
    await vi.waitFor(() => expect(toastTitles()).toContain('The refund was recorded.'))
  })

  it('does not send a refund above the refund due and puts the focus on the amount', async () => {
    mockApi(overpaid)
    const user = userEvent.setup()
    renderPanel()

    await user.click(await screen.findByRole('button', { name: 'Record refund' }))
    const dialog = await screen.findByRole('dialog')
    await user.clear(within(dialog).getByLabelText(/^Amount/))
    await user.type(within(dialog).getByLabelText(/^Amount/), '25,01')
    await user.click(within(dialog).getByRole('button', { name: 'Record refund' }))

    expect(
      await within(dialog).findByText('A refund may pay back at most 25.00 EUR.'),
    ).toBeVisible()
    await vi.waitFor(() => expect(within(dialog).getByLabelText(/^Amount/)).toHaveFocus())
    expect(posts).toHaveLength(0)
  })

  it('does not send a payment date in the future', async () => {
    mockApi(answer([], 0, 23750))
    const user = userEvent.setup()
    renderPanel()

    await user.click(await screen.findByRole('button', { name: 'Record payment' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText(/^Received on/)).toHaveValue(berlinToday())
    await user.clear(within(dialog).getByLabelText(/^Received on/))
    await user.type(within(dialog).getByLabelText(/^Received on/), '2999-01-01')
    await user.type(within(dialog).getByLabelText(/^Amount/), '10')
    await user.click(within(dialog).getByRole('button', { name: 'Record payment' }))

    expect(
      await within(dialog).findByText('The date must be between 2000-01-01 and today.'),
    ).toBeVisible()
    expect(posts).toHaveLength(0)
  })

  it('does not send an amount that is no amount', async () => {
    mockApi(answer([], 0, 23750))
    const user = userEvent.setup()
    renderPanel()

    await user.click(await screen.findByRole('button', { name: 'Record payment' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/^Amount/), 'abc')
    await user.click(within(dialog).getByRole('button', { name: 'Record payment' }))

    expect(await within(dialog).findByText(/Enter an amount above 0/)).toBeVisible()
    expect(posts).toHaveLength(0)
  })

  it('shows the server field errors in the visitor language at their field', async () => {
    mockApi(answer([], 0, 23750))
    vi.mocked(apiClient.post).mockRejectedValue(
      apiError(400, ErrorCode.invalidArgument, {
        amount_cents: 'a refund may pay back at most the refund due, 2500 cents',
        received_on: 'must be between 2000-01-01 and today',
      }),
    )
    const user = userEvent.setup()
    renderPanel()

    await user.click(await screen.findByRole('button', { name: 'Record payment' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/^Amount/), '10')
    await user.click(within(dialog).getByRole('button', { name: 'Record payment' }))

    expect(
      await within(dialog).findByText('A refund may pay back at most the refund due.'),
    ).toBeVisible()
    expect(within(dialog).getByText('The date must be between 2000-01-01 and today.')).toBeVisible()
    expect(screen.queryByText(/a refund may pay back at most the refund due, 2500/)).toBeNull()
    await vi.waitFor(() => expect(within(dialog).getByLabelText(/^Received on/)).toHaveFocus())
  })

  describe('reversal', () => {
    it('reverses a payment with the negated amount, the same day and its id', async () => {
      mockApi(answer([paid], 10000, 13750), answer([paid], 10000, 13750))
      const user = userEvent.setup()
      renderPanel()

      await user.click(
        await screen.findByRole('button', {
          name: 'Reverse the entry of 2026-12-03 over 100.00 EUR',
        }),
      )
      const dialog = await screen.findByRole('dialog')
      expect(within(dialog).getByText('2026-12-03, 100.00 EUR')).toBeVisible()
      expect(posts).toHaveLength(0)
      await user.click(within(dialog).getByRole('button', { name: 'Reverse' }))

      await vi.waitFor(() => expect(posts).toHaveLength(1))
      expect(posts[0]).toEqual({
        path: '/documents/d-1/payments',
        body: {
          received_on: '2026-12-03',
          amount_cents: -10000,
          method: 'bank_transfer',
          note: null,
          reverses_payment_id: 'p-1',
        },
      })
    })

    it('reverses a refund with a positive amount', async () => {
      const refund = payment({ id: 'p-9', amount_cents: -2500, received_on: '2026-12-10' })
      mockApi(answer([paid, refund], 7500, -2500))
      const user = userEvent.setup()
      renderPanel()

      await user.click(
        await screen.findByRole('button', {
          name: 'Reverse the entry of 2026-12-10 over -25.00 EUR',
        }),
      )
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Reverse' }),
      )

      await vi.waitFor(() => expect(posts).toHaveLength(1))
      expect(posts[0]?.body).toMatchObject({
        amount_cents: 2500,
        received_on: '2026-12-10',
        reverses_payment_id: 'p-9',
      })
    })

    it('offers no reversal for a reversed payment or for a reversal row', async () => {
      const reversal = payment({
        id: 'p-3',
        amount_cents: -10000,
        reverses_payment_id: 'p-1',
      })
      mockApi(answer([paid, reversal], 0, 23750))
      renderPanel()

      const table = await screen.findByRole('table', { name: 'Payments of this invoice' })
      expect(within(table).queryByRole('button', { name: /^Reverse/ })).toBeNull()
      expect(within(table).getByText('Reversed')).toBeVisible()
      expect(within(table).getByText('Reversal')).toBeVisible()
    })

    it('tells a conflict and reloads the list', async () => {
      mockApi(answer([paid], 10000, 13750))
      vi.mocked(apiClient.post).mockRejectedValue(apiError(409, ErrorCode.conflict))
      const user = userEvent.setup()
      renderPanel()

      await user.click(await screen.findByRole('button', { name: /^Reverse the entry/ }))
      await user.click(
        within(await screen.findByRole('dialog')).getByRole('button', { name: 'Reverse' }),
      )

      await vi.waitFor(() =>
        expect(toastTitles()).toContainEqual(expect.stringContaining('changed in the meantime')),
      )
      await vi.waitFor(() => expect(apiClient.get).toHaveBeenCalledTimes(2))
    })
  })

  describe('roles and the session', () => {
    it('shows no entry actions to a member', async () => {
      mockApi(answer([paid], 10000, 13750))
      renderPanel({ isOwner: false })

      await screen.findByRole('table', { name: 'Payments of this invoice' })
      expect(screen.queryByRole('button', { name: 'Record payment' })).toBeNull()
      expect(screen.queryByRole('button', { name: /^Reverse/ })).toBeNull()
      expect(screen.getByText('Only the owner records payments.')).toBeVisible()
    })

    it('keeps every write disabled without a session and says why', async () => {
      me.value = null
      sessionFailed.value = true
      mockApi(answer([paid], 10000, 13750))
      renderPanel()

      expect(await screen.findByRole('button', { name: 'Record payment' })).toBeDisabled()
      expect(screen.getByRole('button', { name: 'Record refund' })).toBeDisabled()
      expect(screen.getByRole('button', { name: /^Reverse the entry/ })).toBeDisabled()
      expect(screen.getByText(/The session could not be loaded/)).toBeVisible()
    })

    it('does not post when the session is gone by the time the form is sent', async () => {
      mockApi(answer([], 0, 23750))
      const user = userEvent.setup()
      renderPanel()

      await user.click(await screen.findByRole('button', { name: 'Record payment' }))
      const dialog = await screen.findByRole('dialog')
      await user.type(within(dialog).getByLabelText(/^Amount/), '10')
      me.value = null
      sessionFailed.value = true
      await user.click(within(dialog).getByRole('button', { name: 'Record payment' }))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(posts).toHaveLength(0)
    })
  })

  describe('loading', () => {
    it('shows the error state and retries', async () => {
      vi.mocked(apiClient.get).mockRejectedValue(apiError(500, ErrorCode.internal))
      const user = userEvent.setup()
      renderPanel()

      expect(await screen.findByRole('alert')).toHaveTextContent(
        'The payments could not be loaded.',
      )
      mockApi(answer([paid], 10000, 13750))
      await user.click(screen.getByRole('button', { name: 'Try again' }))

      expect(await screen.findByRole('table', { name: 'Payments of this invoice' })).toBeVisible()
    })

    it('clears the session and goes to the login on a 401', async () => {
      vi.mocked(apiClient.get).mockRejectedValue(apiError(401, ErrorCode.unauthenticated))
      const { router } = renderPanel()

      await vi.waitFor(() => expect(me.value).toBeNull())
      await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    })

    it('ignores an older answer, a failure and a 401 that arrive after a newer request', async () => {
      const calls: { resolve: (v: unknown) => void; reject: (e: unknown) => void }[] = []
      vi.mocked(apiClient.get).mockImplementation(
        () =>
          new Promise((resolve, reject) => {
            calls.push({ resolve, reject })
          }),
      )
      const { rerender, router } = renderPanel()
      const push = vi.spyOn(router, 'push')
      await vi.waitFor(() => expect(calls).toHaveLength(1))
      await rerender({ documentId: 'd-2' })
      await vi.waitFor(() => expect(calls).toHaveLength(2))

      calls[1]?.resolve(answer([payment({ id: 'p-new', amount_cents: 7700 })], 7700, 0))
      expect(await screen.findByText('77.00 EUR', { selector: 'td' })).toBeVisible()
      calls[0]?.reject(apiError(401, ErrorCode.unauthenticated))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(screen.queryByRole('alert')).toBeNull()
      expect(screen.getByText('77.00 EUR', { selector: 'td' })).toBeVisible()
      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
    })

    it('keeps the newer answer when an older one resolves late', async () => {
      const calls: { resolve: (v: unknown) => void }[] = []
      vi.mocked(apiClient.get).mockImplementation(
        () =>
          new Promise((resolve) => {
            calls.push({ resolve })
          }),
      )
      const { rerender } = renderPanel()
      await vi.waitFor(() => expect(calls).toHaveLength(1))
      await rerender({ documentId: 'd-2' })
      await vi.waitFor(() => expect(calls).toHaveLength(2))

      calls[1]?.resolve(answer([payment({ id: 'p-new', amount_cents: 7700 })], 7700, 0))
      await screen.findByText('77.00 EUR', { selector: 'td' })
      calls[0]?.resolve(answer([payment({ id: 'p-old', amount_cents: 1100 })], 1100, 0))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(screen.queryByText('11.00 EUR', { selector: 'td' })).toBeNull()
      expect(screen.getByText('77.00 EUR', { selector: 'td' })).toBeVisible()
    })

    it('aborts the read on unmount and changes nothing when a 401 arrives afterwards', async () => {
      let reject: (error: unknown) => void = () => {}
      vi.mocked(apiClient.get).mockImplementation(
        () =>
          new Promise((_resolve, rej) => {
            reject = rej
          }),
      )
      const { router, unmount } = renderPanel()
      await vi.waitFor(() => expect(apiClient.get).toHaveBeenCalledTimes(1))
      const signal = (vi.mocked(apiClient.get).mock.calls[0]?.[1] as { signal: AbortSignal }).signal
      expect(signal.aborted).toBe(false)

      const push = vi.spyOn(router, 'push')
      const replace = vi.spyOn(router, 'replace')
      unmount()
      expect(signal.aborted).toBe(true)
      reject(apiError(401, ErrorCode.unauthenticated))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
      expect(replace).not.toHaveBeenCalled()
    })

    it('goes nowhere when a 401 on a write arrives after unmount', async () => {
      mockApi(answer([], 0, 23750))
      let reject: (error: unknown) => void = () => {}
      vi.mocked(apiClient.post).mockImplementation(
        () =>
          new Promise((_resolve, rej) => {
            reject = rej
          }),
      )
      const user = userEvent.setup()
      const { router, unmount } = renderPanel()

      await user.click(await screen.findByRole('button', { name: 'Record payment' }))
      const dialog = await screen.findByRole('dialog')
      await user.type(within(dialog).getByLabelText(/^Amount/), '10')
      await user.click(within(dialog).getByRole('button', { name: 'Record payment' }))
      await vi.waitFor(() => expect(apiClient.post).toHaveBeenCalledTimes(1))

      const push = vi.spyOn(router, 'push')
      const replace = vi.spyOn(router, 'replace')
      unmount()
      reject(apiError(401, ErrorCode.unauthenticated))
      await new Promise((resolve) => setTimeout(resolve, 20))

      expect(me.value).not.toBeNull()
      expect(push).not.toHaveBeenCalled()
      expect(replace).not.toHaveBeenCalled()
    })
  })
})
