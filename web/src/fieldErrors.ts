// Turns what the API answers into texts in the visitor's language. The
// server's reasons are English, so a view never shows them as they arrive.
import { isApiError } from '@ostendorf-cloud/govue'

type Translate = (key: string) => string

/** REASON_KEYS maps every reason the customer and catalogue handlers (and
 *  govue's decoder) can put into `details` to a key under
 *  `common.fieldErrors`. */
export const REASON_KEYS: Record<string, string> = {
  required: 'required',
  'must be de or en': 'invalidLanguage',
  'must be an ISO 3166-1 alpha-2 code': 'invalidCountry',
  'must not be negative': 'negative',
  'must be piece, hour or day': 'invalidUnit',
  'wrong type': 'wrongType',
}

/** translateDetails translates each reason. An unknown reason gets the
 *  generic text rather than raw English. */
export function translateDetails(
  details: Record<string, string>,
  t: Translate,
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [field, reason] of Object.entries(details)) {
    out[field] = t(`common.fieldErrors.${REASON_KEYS[reason] ?? 'invalid'}`)
  }
  return out
}

/** saveErrorText names a failed save. The "check the marked fields" text
 *  is only for an answer that carries field errors or a conflict. */
export function saveErrorText(error: unknown, t: Translate, fieldsKey: string): string {
  if (isApiError(error)) {
    if ((error.status === 400 && Object.keys(error.details).length > 0) || error.status === 409) {
      return t(fieldsKey)
    }
    if (error.status === 403) {
      return t('common.errors.forbidden')
    }
    if (error.status >= 500) {
      return t('common.errors.server')
    }
    if (error.isTransport) {
      return t('common.errors.network')
    }
  }
  return t('common.errors.generic')
}
