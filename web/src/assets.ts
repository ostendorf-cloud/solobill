// Shared pieces of the asset views: the types of the API answers, the reason
// an asset cannot be edited, the 409 codes and the translation of the server's
// field reasons. The browser never computes a schedule, a book value or
// whether an asset is editable: the asset JSON carries book_value_cents,
// book_value_year and editable, and the schedule comes from
// GET /api/v1/assets/{id}/schedule. Typed amounts become integer cents through
// money.ts.
import { isApiError } from '@ostendorf-cloud/govue'

import { translateDetails } from './fieldErrors'
import { formatCents } from './money'

export type AssetMethod = 'linear' | 'gwg' | 'sammelposten' | 'immediate'
export type AssetGroup = 'intangible' | 'office_equipment' | 'other_movable'

/** METHODS is every method of the API. There is no degressive method. */
export const METHODS: readonly AssetMethod[] = ['linear', 'gwg', 'sammelposten', 'immediate']
/** GROUPS is every Anlage AVEÜR group of the API. */
export const GROUPS: readonly AssetGroup[] = ['intangible', 'office_equipment', 'other_movable']

export interface Asset {
  id: string
  name: string
  acquired_on: string
  cost_basis_cents: number
  method: AssetMethod
  group: AssetGroup
  useful_life_months: number | null
  expense_id: string | null
  opening_book_value_cents: number | null
  opening_book_value_year: number | null
  disposed_on: string | null
  disposal_proceeds_cents: number | null
  imported: boolean
  /** The closing book value of the last schedule year that has ended, the
   *  value before any depreciation while none has, 0 from the disposal year
   *  on. Computed by the server. null when the server could not compute it. */
  book_value_cents: number | null
  /** The schedule year book_value_cents closes, or the disposal year. null
   *  while none has ended. */
  book_value_year: number | null
  /** The book value left at the end of the disposal year, null unless the
   *  asset is disposed and not a Sammelposten. */
  disposal_rest_cents: number | null
  /** Whether a PUT would be accepted now. Decided by the server. */
  editable: boolean
  created_at: string
  updated_at: string
}

export interface ScheduleYear {
  year: number
  opening_book_value_cents: number
  depreciation_cents: number
  closing_book_value_cents: number
  euer_key: string
}

export interface Schedule {
  asset_id: string
  method: AssetMethod
  years: ScheduleYear[]
}

const BERLIN = 'Europe/Berlin'

/** germanYear is the calendar year of now in Germany, the year an EÜR is
 *  filed for. The server decides the same way, so a late evening of 31
 *  December in UTC is already the next year here. */
export function germanYear(now: Date = new Date()): number {
  const text = new Intl.DateTimeFormat('en-US', { timeZone: BERLIN, year: 'numeric' }).format(now)
  return Number.parseInt(text, 10)
}

/** germanToday is the calendar date of now in Germany as YYYY-MM-DD. */
export function germanToday(now: Date = new Date()): string {
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: BERLIN,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(now)
}

export type EditBlock = 'disposed' | 'yearEnded'

/** editBlock tells why an asset cannot be edited any more, or null while it
 *  can. It reads the server's editable flag and takes no clock: a disposed
 *  asset names the disposal, any other blocked asset the ended year. */
export function editBlock(asset: Asset): EditBlock | null {
  if (asset.editable) {
    return null
  }
  return asset.disposed_on === null ? 'yearEnded' : 'disposed'
}

/** AssetConflict is the reason of a 409 of the asset routes, read from its
 *  code in internal/httpapi/routes_assets.go. 'other' is a plain conflict: the
 *  asset changed meanwhile. */
export type AssetConflict = 'disposed' | 'yearEnded' | 'expenseTaken' | 'other'

const CONFLICT_OF_CODE: Record<string, AssetConflict> = {
  asset_disposed: 'disposed',
  asset_year_ended: 'yearEnded',
  expense_already_an_asset: 'expenseTaken',
}

/** assetConflict tells the reason of a 409, or null for any other error. */
export function assetConflict(error: unknown): AssetConflict | null {
  if (!isApiError(error) || error.status !== 409) {
    return null
  }
  return CONFLICT_OF_CODE[error.code] ?? 'other'
}

/** isRegimeMissing tells the 400 "no VAT regime period covers the payment
 *  date of the expense". */
export function isRegimeMissing(error: unknown): boolean {
  return isApiError(error) && error.status === 400 && error.code === 'vat_regime_missing'
}

type Translate = (key: string, named?: Record<string, unknown>) => string

/** bookValueNote says what the server's book value stands for: missing, the
 *  disposal year, the end of book_value_year, or the value before any
 *  depreciation. */
export function bookValueNote(asset: Asset, t: Translate): string {
  if (asset.book_value_cents === null) {
    return t('assets.bookValue.unavailable')
  }
  if (asset.disposal_rest_cents !== null && asset.book_value_year !== null) {
    return t('assets.bookValue.disposedIn', { year: asset.book_value_year })
  }
  return asset.book_value_year === null
    ? t('assets.bookValue.beforeDepreciation')
    : t('assets.bookValue.atEndOf', { year: asset.book_value_year })
}

/** FIELD_OF maps the API's field names to the form's. */
const FIELD_OF: Record<string, string> = {
  name: 'name',
  acquired_on: 'acquiredOn',
  cost_basis_cents: 'cost',
  method: 'method',
  group: 'group',
  useful_life_months: 'usefulLife',
  expense_id: 'expense',
  opening_book_value_cents: 'opening',
  opening_book_value_year: 'opening',
  disposed_on: 'disposedOn',
  proceeds_cents: 'proceeds',
}

// Reasons of internal/httpapi/routes_assets.go and internal/afa/afa.go, as
// exact text or pattern, each mapped to a key under assets.reasons.
const EXACT_REASONS: Record<string, string> = {
  'too long': 'tooLong',
  'must be a date such as 2026-03-10': 'invalidDate',
  'must be a date such as 2026-12-01': 'invalidDate',
  'must be a date between 1900 and 9999': 'dateRange',
  'must not be in the future': 'future',
  'must be an expense id': 'noExpense',
  'no such expense': 'noExpense',
  'expenses are not available yet': 'expensesUnavailable',
  'must be a booked expense': 'notBooked',
  'must be an expense with is_asset set': 'notAsset',
  'must not be a storno row': 'storno',
  'the expense amounts do not give a cost basis': 'noBasis',
  'must be positive': 'positive',
  'must be linear, gwg, sammelposten or immediate': 'invalidMethod',
  'must be intangible, office_equipment or other_movable': 'invalidGroup',
  'immediate is only allowed up to 250.00 EUR': 'immediateLimit',
  'gwg is only allowed up to 800.00 EUR': 'gwgLimit',
  'sammelposten is only allowed above 250.00 EUR up to 1,000.00 EUR': 'poolRange',
  'only allowed for linear': 'linearOnly',
  'opening_book_value_cents and opening_book_value_year go together': 'openingTogether',
  'must not be after the current year': 'openingYearFuture',
  'must be after the year of acquired_on': 'openingYearEarly',
  'must be from 0 to cost_basis_cents': 'openingRange',
  'must not be before acquired_on': 'beforeAcquired',
  'must not be before opening_book_value_year': 'beforeOpening',
  'must be a date before 10000': 'dateRange',
  'must not be negative': 'negative',
}
const PATTERN_REASONS: [RegExp, string][] = [
  [/^required for linear, from 1 to \d+$/, 'usefulLifeRequired'],
  [/^must be 0: /, 'openingZero'],
]
const BASIS_REASON = /^must be (\d+), the expense's cost basis under the (\w+) regime$/

/** assetFieldErrors turns the `details` of an answer into texts keyed by the
 *  form's field names. A reason this module does not know falls back to the
 *  shared translation, never to raw English. */
export function assetFieldErrors(
  details: Record<string, string>,
  t: Translate,
  locale: string,
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [apiField, reason] of Object.entries(details)) {
    const field = FIELD_OF[apiField] ?? apiField
    const basis = BASIS_REASON.exec(reason)
    if (basis) {
      // The figure is the server's own: it is only written out, never
      // recomputed.
      out[field] = t('assets.reasons.basisMismatch', {
        amount: formatCents(Number.parseInt(basis[1] ?? '0', 10), locale),
        regime: t(`assets.regime.${basis[2] === 'standard' ? 'standard' : 'kleinunternehmer'}`),
      })
      continue
    }
    const own = EXACT_REASONS[reason] ?? PATTERN_REASONS.find(([re]) => re.test(reason))?.[1]
    out[field] = own
      ? t(`assets.reasons.${own}`)
      : (translateDetails({ [apiField]: reason }, (key) => t(key))[apiField] ?? '')
  }
  return out
}
