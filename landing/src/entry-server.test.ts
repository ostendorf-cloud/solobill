// @vitest-environment node
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { beforeAll, describe, expect, it } from 'vitest'

import { files } from './entry-server'

// The prerender entry is what govue-prerender --out-dir calls. These tests run
// it against the real index.html template, so the pages, the sitemap and the
// 404 page of each language are the ones the Go server then has to find.

const template = readFileSync(fileURLToPath(new URL('../index.html', import.meta.url)), 'utf-8')

let output: Map<string, string>

beforeAll(async () => {
  const list = await files(template)
  output = new Map(list.map((f) => [f.path, f.content]))
})

describe('files', () => {
  it('writes every route in every language, the sitemap and a 404 page per language', () => {
    expect([...output.keys()].sort()).toEqual([
      'de/404.html',
      'de/imprint/index.html',
      'de/index.html',
      'de/privacy/index.html',
      'en/404.html',
      'en/imprint/index.html',
      'en/index.html',
      'en/privacy/index.html',
      'sitemap.xml',
    ])
  })

  it('writes no terms page, the site sells nothing', () => {
    expect([...output.keys()].filter((p) => p.includes('terms'))).toEqual([])
  })

  it('lists each page of the sitemap without a trailing slash except the language root', () => {
    const locs = [...(output.get('sitemap.xml') ?? '').matchAll(/<loc>([^<]+)<\/loc>/g)].map(
      (m) => m[1],
    )
    expect(locs).toEqual([
      'https://solobill.de/de/',
      'https://solobill.de/en/',
      'https://solobill.de/de/imprint',
      'https://solobill.de/en/imprint',
      'https://solobill.de/de/privacy',
      'https://solobill.de/en/privacy',
    ])
  })

  it.each([
    ['de/index.html', 'de'],
    ['en/index.html', 'en'],
    ['de/imprint/index.html', 'de'],
    ['en/privacy/index.html', 'en'],
    ['de/404.html', 'de'],
    ['en/404.html', 'en'],
  ])('sets the language of %s', (path, lang) => {
    expect(output.get(path)).toContain(`<html lang="${lang}"`)
  })

  it.each([
    ['de/index.html', '/de/imprint', '/de/privacy'],
    ['en/index.html', '/en/imprint', '/en/privacy'],
    ['de/imprint/index.html', '/de/imprint', '/de/privacy'],
    ['en/404.html', '/en/imprint', '/en/privacy'],
  ])('links the legal pages in the footer of %s', (path, imprint, privacy) => {
    const html = output.get(path) ?? ''
    expect(html).toContain(`href="${imprint}"`)
    expect(html).toContain(`href="${privacy}"`)
  })

  it('keeps the 404 page out of an index and gives it no canonical', () => {
    for (const path of ['de/404.html', 'en/404.html']) {
      const html = output.get(path) ?? ''
      expect(html).toContain('<meta name="robots" content="noindex">')
      expect(html).not.toContain('rel="canonical"')
      expect(html).not.toContain('rel="alternate"')
    }
  })

  it.each([
    ['de/index.html', 'Anmelden', 'Registrieren'],
    ['en/index.html', 'Log in', 'Sign up'],
  ])(
    'writes the default login link and a hidden register button in %s',
    (path, login, register) => {
      const html = output.get(path) ?? ''
      expect(html).toContain(`href="https://app.solobill.de/login"`)
      expect(html).toContain(login)
      // Reserved but invisible: the configuration decides at runtime.
      expect(html).toMatch(
        new RegExp(
          `<a[^>]*href="https://app.solobill.de/register"[^>]*invisible[^>]*>${register}</a>`,
        ),
      )
    },
  )

  it('leaves no placeholder behind', () => {
    for (const [path, html] of output) {
      if (path === 'sitemap.xml') continue
      expect(html, path).not.toContain('<!--app-html-->')
      expect(html, path).not.toContain('<!--app-head-->')
    }
  })

  it('writes the favicon links and the theme boot script nowhere, the server injects them', () => {
    for (const [path, html] of output) {
      if (path === 'sitemap.xml') continue
      expect(html, path).not.toContain('rel="icon"')
      expect(html, path).not.toContain('name="theme-color"')
    }
  })
})
