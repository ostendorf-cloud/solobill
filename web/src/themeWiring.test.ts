import { describe, expect, it } from 'vitest'

import indexHtml from '../index.html?raw'
import mainSource from './main.ts?raw'

// Two files have to name the same theme: index.html (what the first paint
// and the server's boot script fall back to) and main.ts (what useTheme sets
// once the bundle runs). A mismatch shows as a flash of govue's sample
// theme, so each is read from its source and compared with the other.

function rootThemeAttribute(): string | undefined {
  return /<html\b[^>]*\sdata-theme="([^"]+)"/.exec(indexHtml)?.[1]
}

function themePassedToUseTheme(): string | undefined {
  return /useTheme\(\s*\{[^}]*\btheme:\s*'([^']+)'/.exec(mainSource)?.[1]
}

describe('theme wiring', () => {
  it('names the brand theme on the root element in index.html', () => {
    expect(rootThemeAttribute()).toBe('solobill')
  })

  it('passes the same theme name to useTheme in main.ts', () => {
    expect(themePassedToUseTheme()).toBe(rootThemeAttribute())
  })
})
