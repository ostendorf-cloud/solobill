import { createGovueI18n, localeCodes, type LocaleCode } from '@ostendorf-cloud/govue'
import {
  HEAD_PLACEHOLDER,
  injectIntoTemplate,
  localizedFiles,
  render,
  type PrerenderFile,
} from '@ostendorf-cloud/govue/server'
import type { Component } from 'vue'

import { componentFor, NotFoundPage, routes } from './pages'
import { SITE_ORIGIN } from './site'
import { startTheme } from './theme'

/**
 * The SSR entry of the site, the shape `govue-prerender --out-dir` loads:
 * the build is `vite build --ssr src/entry-server.ts`, and the CLI calls
 * files() with the built index.html. After it `govue-check-legal --dir dist`
 * checks the legal pages and the footer links.
 */

function renderPage(component: Component, locale: LocaleCode): Promise<string> {
  return render(component, {
    setup: (app) => {
      app.use(createGovueI18n({ locale }))
    },
  })
}

/**
 * The 404 page of one language, written as <code>/404.html. govue's
 * localizedFiles writes the routes of the table and the sitemap, and the 404
 * page answers no route, so the site writes it. It carries no canonical and
 * no hreflang, and a robots tag keeps it out of an index.
 */
async function notFoundFile(template: string, locale: LocaleCode): Promise<PrerenderFile> {
  const tag = /<html lang="[^"]*"/
  if (!tag.test(template)) {
    throw new Error('landing: the template has no <html lang="..."> start tag')
  }
  const withLang = template.replace(tag, `<html lang="${locale}"`)
  const withHead = injectIntoTemplate(
    withLang,
    '<meta name="robots" content="noindex">',
    HEAD_PLACEHOLDER,
  )
  const html = await renderPage(NotFoundPage, locale)
  return { path: `${locale}/404.html`, content: injectIntoTemplate(withHead, html) }
}

export async function files(template: string): Promise<PrerenderFile[]> {
  startTheme()
  const pages = await localizedFiles(routes, {
    origin: SITE_ORIGIN,
    template,
    render: (variant) => renderPage(componentFor(variant), variant.locale),
  })
  const notFound = await Promise.all(localeCodes.map((code) => notFoundFile(template, code)))
  return [...pages, ...notFound]
}
