import { beforeEach, describe, expect, it, vi } from 'vitest'

// The colour mode lives in module state of govue's useTheme, and only its
// first call reads the stored choice. Every test therefore resets the module
// registry and imports Vue, govue and the view fresh, as a page load would.

vi.mock('../api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const THEME_KEY = 'govue-theme'
const LANGUAGE_KEY = 'govue-language'

async function renderView(path: string, signedIn = true) {
  const govue = await import('@ostendorf-cloud/govue')
  const { apiClient } = await import('../api')
  vi.mocked(apiClient.get).mockImplementation((path: string) => {
    if (path === '/me' && signedIn) {
      return Promise.resolve({ email: 'a@b.test', name: 'Ada', role: 'member', csrf_token: 'c' })
    }
    return Promise.reject(
      path === '/me'
        ? new govue.ApiError({
            code: govue.ErrorCode.unauthenticated,
            message: 'log in first',
            status: 401,
          })
        : new Error('no status'),
    )
  })
  const { render, screen } = await import('@testing-library/vue')
  const { default: userEvent } = await import('@testing-library/user-event')
  const { createI18n } = await import('vue-i18n')
  const { createMemoryHistory, createRouter } = await import('vue-router')
  const { de, en } = await import('../i18n/locales')
  const { followUrlLocale, withLocale } = await import('../router/locale')
  const { accountRoutes } = await import('../router/routes/account')
  const { default: AccountView } = await import('./AccountView.vue')

  // What main.ts does before the app mounts.
  govue.useTheme({ theme: 'solobill' })

  const stub = { template: '<main>page</main>' }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: withLocale(
      [
        { path: accountRoutes[0].path, name: accountRoutes[0].name, component: AccountView },
        { path: '/login', name: 'login', component: stub },
      ],
      () => router,
      () => 'en',
    ),
  })
  const locale = path.startsWith('/de') ? 'de' : 'en'
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  const govueI18n = govue.createGovueI18n({ i18n })
  followUrlLocale(router, govueI18n.language)
  await router.push(path)
  await router.isReady()
  render({ template: '<RouterView />' }, { global: { plugins: [router, i18n, govueI18n] } })
  return { router, i18n, screen, user: userEvent.setup() }
}

function stored(key: string): unknown {
  const raw = localStorage.getItem(key)
  return raw === null ? null : JSON.parse(raw)
}

describe('AccountView', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.clearAllMocks()
    localStorage.clear()
    delete document.documentElement.dataset.mode
    delete document.documentElement.dataset.theme
  })

  it('shows the language and the colour mode with their current values', async () => {
    const { screen } = await renderView('/en/account')

    expect(await screen.findByRole('heading', { name: 'Account settings', level: 1 })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Language', level: 2 })).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Appearance', level: 2 })).toBeVisible()
    expect(screen.getByTestId('current-language')).toHaveTextContent('Current language: English')
    expect(screen.getByTestId('current-mode')).toHaveTextContent(
      'Current appearance: Follow the system',
    )
    expect(screen.getByRole('radio', { name: 'Follow the system' })).toBeChecked()
    expect(screen.getByRole('radiogroup', { name: 'Choose the appearance' })).toBeVisible()
    expect(screen.getByRole('group', { name: 'Choose the language' })).toBeVisible()
  })

  it('applies a chosen colour mode at once and remembers it under the boot script key', async () => {
    const { screen, user } = await renderView('/en/account')

    await user.click(await screen.findByRole('radio', { name: 'Dark' }))

    expect(document.documentElement.dataset.mode).toBe('dark')
    expect(screen.getByRole('radio', { name: 'Dark' })).toBeChecked()
    expect(screen.getByTestId('current-mode')).toHaveTextContent('Current appearance: Dark')
    expect(stored(THEME_KEY)).toEqual({ theme: 'solobill', mode: 'dark' })

    await user.click(screen.getByRole('radio', { name: 'Follow the system' }))
    expect(document.documentElement.dataset.mode).toBeUndefined()
  })

  it('starts in the remembered mode on the next visit', async () => {
    localStorage.setItem(THEME_KEY, JSON.stringify({ theme: 'solobill', mode: 'light' }))

    const { screen } = await renderView('/en/account')

    expect(await screen.findByRole('radio', { name: 'Light' })).toBeChecked()
    expect(screen.getByTestId('current-mode')).toHaveTextContent('Current appearance: Light')
    expect(document.documentElement.dataset.mode).toBe('light')
  })

  it('moves the colour mode with the arrow keys and keeps focus on the choice', async () => {
    const { screen, user } = await renderView('/en/account')

    const system = await screen.findByRole('radio', { name: 'Follow the system' })
    system.focus()
    await user.keyboard('{ArrowRight}')

    const light = screen.getByRole('radio', { name: 'Light' })
    expect(light).toBeChecked()
    expect(light).toHaveFocus()
    expect(document.documentElement.dataset.mode).toBe('light')
  })

  it('labels the controls in German', async () => {
    const { screen } = await renderView('/de/account')

    expect(await screen.findByRole('heading', { name: 'Kontoeinstellungen' })).toBeVisible()
    expect(screen.getByRole('radiogroup', { name: 'Darstellung wählen' })).toBeVisible()
    expect(screen.getByRole('radio', { name: 'Dunkel' })).toBeVisible()
    expect(screen.getByTestId('current-language')).toHaveTextContent('Aktuelle Sprache: Deutsch')
  })

  it('switches the language by the URL, stays on the page and remembers it', async () => {
    const { screen, router, i18n, user } = await renderView('/en/account')

    const group = await screen.findByRole('group', { name: 'Choose the language' })
    expect(group.querySelector('a[hreflang="en"]')).toHaveAttribute('aria-current', 'true')
    await user.click(screen.getByRole('link', { name: 'Deutsch' }))

    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/de/account'))
    expect(i18n.global.locale.value).toBe('de')
    expect(document.documentElement.lang).toBe('de')
    expect(localStorage.getItem(LANGUAGE_KEY)).toBe('de')
    expect(await screen.findByRole('heading', { name: 'Kontoeinstellungen' })).toBeVisible()
    expect(screen.getByTestId('current-language')).toHaveTextContent('Aktuelle Sprache: Deutsch')
  })

  it('sends a visitor without a session to the login', async () => {
    const { router } = await renderView('/en/account', false)

    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
  })
})
