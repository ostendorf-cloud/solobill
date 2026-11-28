import { useTheme } from '@ostendorf-cloud/govue'

/**
 * The one brand theme of the site, named in index.html on the root element
 * and in src/theme/solobill.css. Mode stays on "system" until a visitor
 * picks one: useTheme keeps the choice under its default storage key, which
 * the boot script of the frontend server reads before the first paint.
 */
export const THEME_NAME = 'solobill'

/**
 * Starts the theme state. The browser entry and the prerender entry both call
 * it, so the theme switch is prerendered in the state the client starts in.
 */
export function startTheme(): void {
  useTheme({ theme: THEME_NAME })
}
