/** The EÜR report JSON of GET /api/v1/reports/euer/{year} (docs/api.md,
 *  "EÜR report"), and the few pure helpers the report view needs. The
 *  browser computes no figure: every amount comes from the server. */

export type EuerSourceKind = 'payment' | 'expense' | 'other_income' | 'asset'

export interface EuerSource {
  kind: EuerSourceKind
  id: string
  date: string
  amount_cents: number
}

export interface EuerLine {
  key: string
  zeile: number
  kennzahl: string
  /** '-' or '+' where the form prints one next to the Kennzahl, else ''. */
  operator: string
  verified: boolean
  label_de: string
  label_en: string
  amount_cents: number
  sources: EuerSource[]
}

export interface EuerBoundaryEntry {
  kind: Exclude<EuerSourceKind, 'asset'>
  id: string
  date: string
  amount_cents: number
  assigned_tax_year: number | null
}

export interface EuerWarning {
  code: 'missing_receipt' | 'asset_expense_without_asset' | 'unassigned_boundary_entry'
  kind: EuerSourceKind
  id: string
  related_ids: string[]
}

export interface EuerReport {
  year: number
  mapping: { tax_year: number; status: 'verified' | 'unverified'; source: string }
  vat_regimes: { valid_from: string; regime: 'kleinunternehmer' | 'standard' }[]
  lines: EuerLine[]
  boundary_entries: EuerBoundaryEntry[]
  warnings: EuerWarning[]
  disclaimer: string
}

export interface EuerAssignment {
  id: string
  entry_kind: EuerBoundaryEntry['kind']
  entry_id: string
  tax_year: number
  note: string | null
  supersedes_id: string | null
  created_at: string
}

/** FIRST_REPORT_YEAR is the first year solobill reports. The imported years
 *  2023 to 2025 are archive only. */
export const FIRST_REPORT_YEAR = 2026

/** reportYears lists the years the year picker offers: 2026 up to the year
 *  of today, newest first. */
export function reportYears(today: Date): number[] {
  const last = Math.max(FIRST_REPORT_YEAR, today.getFullYear())
  const out: number[] = []
  for (let y = last; y >= FIRST_REPORT_YEAR; y--) {
    out.push(y)
  }
  return out
}

/** otherYear is the year an entry of the turn of the year may move to: the
 *  next year for a December date, the previous one for a January date. */
export function otherYear(date: string): number {
  const year = Number(date.slice(0, 4))
  return date.slice(5, 7) === '12' ? year + 1 : year - 1
}

/** countedYear is the year an entry counts in: its assignment, or the year
 *  of its date. */
export function countedYear(entry: EuerBoundaryEntry): number {
  return entry.assigned_tax_year ?? Number(entry.date.slice(0, 4))
}

/** euerCsvUrl is the CSV export of a year. */
export function euerCsvUrl(year: number): string {
  return `/api/v1/reports/euer/${year}/csv`
}
