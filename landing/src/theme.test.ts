import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import indexHtml from '../index.html?raw'
import { THEME_NAME } from './theme'

function read(relative: string): string {
  return readFileSync(fileURLToPath(new URL(relative, import.meta.url)), 'utf-8')
}

describe('theme', () => {
  it('names the brand theme on the root element in index.html', () => {
    expect(/<html\b[^>]*\sdata-theme="([^"]+)"/.exec(indexHtml)?.[1]).toBe(THEME_NAME)
  })

  // src/theme/ is a copy of the app's theme, so the site and the app look
  // like one product. A brand change regenerates web/src/theme/ first and
  // copies the two files here.
  it.each(['solobill.css', 'seed.json'])("keeps %s equal to the app's copy", (name) => {
    expect(read(`./theme/${name}`)).toBe(read(`../../web/src/theme/${name}`))
  })
})
