import { beforeEach, describe, expect, it, vi } from 'vitest'

// The colour mode lives in module state of govue's useTheme, and only its
// first call reads the stored choice. Every test therefore resets the module
// registry and imports Vue, govue and the app fresh, as a page load would.

vi.mock('./api', () => ({
  apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() },
  csrfToken: { value: undefined },
}))

const STORAGE_KEY = 'govue-theme'

async function renderShell(locale: 'en' | 'de', route = '/') {
  const { apiClient } = await import('./api')
  vi.mocked(apiClient.get).mockImplementation((path: string) =>
    path === '/me'
      ? Promise.resolve({ email: 'a@b.test', name: 'Ada', role: 'owner', csrf_token: 'csrf' })
      : Promise.reject(new Error('no status')),
  )
  const { createGovueI18n, useTheme } = await import('@ostendorf-cloud/govue')
  const { render, screen } = await import('@testing-library/vue')
  const { default: userEvent } = await import('@testing-library/user-event')
  const { createI18n } = await import('vue-i18n')
  const { createMemoryHistory, createRouter } = await import('vue-router')
  const { de, en } = await import('./i18n/locales')
  const { default: App } = await import('./App.vue')

  // What main.ts does before the app mounts.
  useTheme({ theme: 'solobill' })

  const stub = { template: '<main id="main">page</main>' }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: stub },
      { path: '/login', name: 'login', component: stub },
    ],
  })
  const i18n = createI18n({ legacy: false, locale, messages: { en, de } })
  await router.push(route)
  await router.isReady()
  render(App, { global: { plugins: [router, i18n, createGovueI18n({ i18n })] } })
  return { screen, user: userEvent.setup() }
}

function stored(): unknown {
  const raw = localStorage.getItem(STORAGE_KEY)
  return raw === null ? null : JSON.parse(raw)
}

describe('App colour mode switch', () => {
  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
    delete document.documentElement.dataset.mode
    delete document.documentElement.dataset.theme
  })

  it('shows one cycle button next to the language switch, naming this and the next mode', async () => {
    const { screen } = await renderShell('en')

    const button = await screen.findByRole('button', {
      name: 'Theme: Follow the system. Next: Light mode',
    })
    expect(button).toBeVisible()
    expect(screen.queryByRole('radiogroup')).toBeNull()
    expect(screen.getByRole('combobox', { name: 'Language' })).toBeVisible()
  })

  it('cycles system, light, dark, system and sets the mode on the root element', async () => {
    const { screen, user } = await renderShell('en')
    const root = document.documentElement

    expect(root.dataset.mode).toBeUndefined()

    await user.click(await screen.findByRole('button', { name: /^Theme: Follow the system/ }))
    expect(root.dataset.mode).toBe('light')
    expect(root.dataset.theme).toBe('solobill')

    await user.click(screen.getByRole('button', { name: /^Theme: Light mode/ }))
    expect(root.dataset.mode).toBe('dark')

    await user.click(screen.getByRole('button', { name: /^Theme: Dark mode/ }))
    expect(root.dataset.mode).toBeUndefined()
    expect(screen.getByRole('button', { name: /^Theme: Follow the system/ })).toBeVisible()
  })

  it('stores the choice under the key the boot script of the server reads', async () => {
    const { screen, user } = await renderShell('en')

    await user.click(await screen.findByRole('button', { name: /^Theme: Follow the system/ }))

    expect(stored()).toEqual({ theme: 'solobill', mode: 'light' })
  })

  it('starts in the remembered mode on the next visit', async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ theme: 'solobill', mode: 'dark' }))

    const { screen } = await renderShell('en')

    expect(
      await screen.findByRole('button', { name: 'Theme: Dark mode. Next: Follow the system' }),
    ).toBeVisible()
    expect(document.documentElement.dataset.mode).toBe('dark')
  })

  it('follows the system on a first visit', async () => {
    const { screen } = await renderShell('en')

    await screen.findByRole('button', { name: /^Theme: Follow the system/ })
    expect(document.documentElement.dataset.mode).toBeUndefined()
    expect(stored()).toBeNull()
  })

  it('labels the button in German', async () => {
    const { screen } = await renderShell('de')

    expect(
      await screen.findByRole('button', {
        name: 'Darstellung: Dem System folgen. Als Nächstes: Heller Modus',
      }),
    ).toBeVisible()
  })

  it('shows no switch on the bare login route', async () => {
    const { screen } = await renderShell('en', '/login')

    await screen.findByRole('main')
    expect(screen.queryByRole('button', { name: /^Theme:/ })).toBeNull()
  })
})
