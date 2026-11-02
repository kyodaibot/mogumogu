import { describe, expect, it } from 'vitest'
import { ApiError } from '../api'
import { setLocale } from '../i18n'
import { memoryErrorText } from './memories'

const invalid = (reason: string) =>
  new ApiError(422, 'validation_failed', '', [{ field: 'content', reason }])

describe('memoryErrorText', () => {
  it('explains each refusal of the memories API', () => {
    setLocale('ja')
    expect(memoryErrorText(invalid('duplicate_item'))).toBe('すでに同じ内容があります。')
    expect(memoryErrorText(invalid('limit_reached'))).toContain('30 件まで')
    expect(memoryErrorText(invalid('invalid_format'))).toContain('100 文字以内')
    expect(memoryErrorText(new ApiError(403, 'pro_required', ''))).toContain('Pro の機能')

    setLocale('en')
    expect(memoryErrorText(invalid('too_long'))).toBe('At most 100 characters, on one line.')
    expect(memoryErrorText(new ApiError(404, 'not_found', ''))).toBe('This record does not exist or was deleted.')
  })
})
