import { createGovueI18n } from '@ostendorf-cloud/govue'
import { render, screen } from '@testing-library/vue'
import { beforeEach, describe, expect, it } from 'vitest'

import { DEFAULT_CONFIG, siteConfig } from '../siteConfig'
import HomePage from './HomePage.vue'

function renderHome(locale: 'de' | 'en' = 'de') {
  return render(HomePage, { global: { plugins: [createGovueI18n({ locale })] } })
}

beforeEach(() => {
  Object.assign(siteConfig, DEFAULT_CONFIG, { loaded: true })
})

describe('the login buttons', () => {
  it('lead to the default login URL with no configuration', () => {
    renderHome()

    // The header's button and the hero's button.
    const links = screen.getAllByRole('link', { name: 'Anmelden' })
    expect(links).toHaveLength(2)
    for (const link of links) {
      expect(link).toHaveAttribute('href', 'https://app.solobill.de/login')
    }
  })

  it('lead to the configured login URL, in the header and in the hero', () => {
    Object.assign(siteConfig, { loginUrl: 'https://staging.solobill.de/sign-in' })
    renderHome('en')

    const links = screen.getAllByRole('link', { name: 'Log in' })
    expect(links).toHaveLength(2)
    for (const link of links) {
      expect(link).toHaveAttribute('href', 'https://staging.solobill.de/sign-in')
    }
  })
})

describe('the register button', () => {
  it('leads to the default register URL with no configuration', () => {
    renderHome()

    expect(screen.getByRole('link', { name: 'Registrieren' })).toHaveAttribute(
      'href',
      'https://app.solobill.de/register',
    )
  })

  it('leads to the configured register URL', () => {
    Object.assign(siteConfig, { registerUrl: 'https://staging.solobill.de/sign-up' })
    renderHome('en')

    expect(screen.getByRole('link', { name: 'Sign up' })).toHaveAttribute(
      'href',
      'https://staging.solobill.de/sign-up',
    )
  })

  it('is gone when registration is off', () => {
    Object.assign(siteConfig, { registrationEnabled: false })
    renderHome()

    expect(screen.queryByRole('link', { name: 'Registrieren' })).not.toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: 'Anmelden' })).toHaveLength(2)
  })

  it('is reserved but hidden and unreachable until the configuration is loaded', () => {
    Object.assign(siteConfig, { loaded: false })
    renderHome()

    // Hidden from the accessibility tree, so getByRole finds it only when asked.
    const link = screen.getByText('Registrieren')
    expect(link).toHaveClass('invisible')
    expect(link).toHaveAttribute('aria-hidden', 'true')
    expect(link).toHaveAttribute('tabindex', '-1')
  })
})
