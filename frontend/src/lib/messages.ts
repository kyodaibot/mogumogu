// User-facing text for API error codes and validation reasons, in the current
// language (src/i18n). The API's own `message` is for developers and is
// never shown.

import { ApiError } from '../api'
import { t } from '../i18n'

/** A general message for any error. */
export function errorMessage(err: unknown): string {
  const e = t.value.errors
  if (err instanceof ApiError) {
    return e.codes[err.code] ?? e.requestFailed(err.status)
  }
  return e.unexpected
}

/**
 * Field messages from a validation error, keyed by field name. Array items
 * such as "allergens[2]" are reported on their array field ("allergens").
 */
export function fieldMessages(err: unknown): Record<string, string> {
  const e = t.value.errors
  const out: Record<string, string> = {}
  if (!(err instanceof ApiError) || err.code !== 'validation_failed') return out
  for (const d of err.details) {
    const field = d.field.replace(/\[\d+\]$/, '')
    const item = d.field !== field ? e.item(Number(d.field.match(/\[(\d+)\]$/)![1]) + 1) : ''
    // Field wording describes the field itself ("allergens/too_long" is about
    // the list), so an item error uses the general reason.
    const specific = item ? undefined : e.fieldReasons[`${field}/${d.reason}`]
    const text = specific ?? e.reasons[d.reason] ?? d.reason
    out[field] = out[field] ? `${out[field]}${e.joiner}${item}${text}` : item + text
  }
  return out
}
