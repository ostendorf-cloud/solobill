/** Input and display of an integer amount in cents. The browser never
 *  computes with money: it parses the typed digits into cents and writes
 *  cents back as text, both on the digit string, never through a float. */

/** parseCents turns "95", "95.5" or "1234,50" style input into integer
 *  cents. It accepts one decimal separator (comma or point) with at most two
 *  digits after it and no thousands separator. null means the text is not
 *  an amount. */
export function parseCents(text: string): number | null {
  const match = /^(\d{1,12})(?:[.,](\d{1,2}))?$/.exec(text.trim())
  if (!match) {
    return null
  }
  const whole = match[1]
  const fraction = (match[2] ?? '').padEnd(2, '0')
  return Number(whole) * 100 + Number(fraction)
}

/** formatCents writes cents as an amount with two decimals in the given
 *  locale, for example "95,00 EUR" in German. The digits come from the
 *  integer by string slicing. */
export function formatCents(cents: number, locale: string): string {
  const negative = cents < 0
  const digits = String(Math.abs(Math.trunc(cents))).padStart(3, '0')
  const whole = digits.slice(0, -2)
  const fraction = digits.slice(-2)
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, locale.startsWith('de') ? '.' : ',')
  const decimal = locale.startsWith('de') ? ',' : '.'
  return `${negative ? '-' : ''}${grouped}${decimal}${fraction} EUR`
}

/** centsToInput writes cents as editable text, the shape parseCents reads
 *  back: "95.00" with a point, whatever the locale. */
export function centsToInput(cents: number): string {
  const digits = String(Math.max(0, Math.trunc(cents))).padStart(3, '0')
  return `${digits.slice(0, -2)}.${digits.slice(-2)}`
}
