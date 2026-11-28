import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  CONFIG_PATH,
  DEFAULT_CONFIG,
  loadSiteConfig,
  parseSiteConfig,
  siteConfig,
} from './siteConfig'

function reply(body: unknown, init: ResponseInit = {}): typeof fetch {
  return vi.fn().mockResolvedValue(new Response(JSON.stringify(body), init))
}

beforeEach(() => {
  Object.assign(siteConfig, DEFAULT_CONFIG, { loaded: false })
})

describe('DEFAULT_CONFIG', () => {
  it('points at the app on app.solobill.de with registration open', () => {
    expect(DEFAULT_CONFIG).toEqual({
      loginUrl: 'https://app.solobill.de/login',
      registerUrl: 'https://app.solobill.de/register',
      registrationEnabled: true,
    })
  })
})

describe('parseSiteConfig', () => {
  it('takes every field of a valid body', () => {
    const body = {
      loginUrl: 'https://staging.solobill.de/sign-in',
      registerUrl: 'https://staging.solobill.de/sign-up',
      registrationEnabled: false,
    }
    expect(parseSiteConfig(body)).toEqual(body)
  })

  it.each([
    ['null', null],
    ['a string', 'x'],
    ['an array', []],
    ['an empty object', {}],
  ])('takes the defaults for %s', (_name, body) => {
    expect(parseSiteConfig(body)).toEqual(DEFAULT_CONFIG)
  })

  it.each([
    ['a script URL', 'javascript:alert(1)'],
    ['a data URL', 'data:text/html,x'],
    ['a relative URL', '/login'],
    ['an empty string', ''],
    ['a number', 7],
  ])('never takes %s as a URL', (_name, value) => {
    const got = parseSiteConfig({ loginUrl: value, registerUrl: value })
    expect(got.loginUrl).toBe(DEFAULT_CONFIG.loginUrl)
    expect(got.registerUrl).toBe(DEFAULT_CONFIG.registerUrl)
  })

  it('takes the default for a switch that is not a boolean', () => {
    expect(parseSiteConfig({ registrationEnabled: 'false' }).registrationEnabled).toBe(true)
  })
})

describe('loadSiteConfig', () => {
  it('asks for /config.json, revalidated, and stores the answer', async () => {
    const fetcher = reply({
      loginUrl: 'https://staging.solobill.de/sign-in',
      registerUrl: 'https://staging.solobill.de/sign-up',
      registrationEnabled: false,
    })

    await loadSiteConfig(fetcher)

    expect(fetcher).toHaveBeenCalledWith(
      CONFIG_PATH,
      expect.objectContaining({ cache: 'no-cache' }),
    )
    expect(siteConfig).toMatchObject({
      loginUrl: 'https://staging.solobill.de/sign-in',
      registerUrl: 'https://staging.solobill.de/sign-up',
      registrationEnabled: false,
      loaded: true,
    })
  })

  // A failed load cannot know whether the server closed the registration, so
  // the register button stays hidden and the login URL stays the default.
  const failedConfig = { ...DEFAULT_CONFIG, registrationEnabled: false, loaded: true }

  it('hides the register button and still marks loaded when the server answers an error', async () => {
    await loadSiteConfig(reply({ registrationEnabled: true }, { status: 500 }))

    expect(siteConfig).toMatchObject(failedConfig)
  })

  it('hides the register button when the request fails', async () => {
    await loadSiteConfig(vi.fn().mockRejectedValue(new TypeError('network')))

    expect(siteConfig).toMatchObject(failedConfig)
  })

  it('hides the register button when the request times out', async () => {
    await loadSiteConfig(vi.fn().mockRejectedValue(new DOMException('timeout', 'TimeoutError')))

    expect(siteConfig).toMatchObject(failedConfig)
  })

  it('hides the register button when the body is not JSON', async () => {
    await loadSiteConfig(vi.fn().mockResolvedValue(new Response('<html>')))

    expect(siteConfig).toMatchObject(failedConfig)
  })
})
