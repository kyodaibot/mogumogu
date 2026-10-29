import { afterEach, describe, expect, it } from 'vitest'
import { ApiError } from '../api'
import { errorMessage, fieldMessages } from '../lib/messages'
import { MEAL_TYPES } from '../lib/meals'
import { detectLocale, setLocale, t } from '.'
import { en } from './en'
import { ja } from './ja'

describe('detectLocale', () => {
  it('prefers a saved choice', () => {
    expect(detectLocale('ja', ['en-US'])).toBe('ja')
    expect(detectLocale('en', ['ja-JP'])).toBe('en')
  })
  it('takes the first browser language that is Japanese or English', () => {
    expect(detectLocale(null, ['ja-JP', 'en-US'])).toBe('ja')
    expect(detectLocale(null, ['fr-FR', 'ja'])).toBe('ja')
    expect(detectLocale(null, ['en-GB', 'ja-JP'])).toBe('en')
  })
  it('falls back to English, ignoring unknown saved values', () => {
    expect(detectLocale(null, ['fr-FR', 'zh-CN'])).toBe('en')
    expect(detectLocale('zh', [])).toBe('en')
    expect(detectLocale(null, ['javanese'])).toBe('en')
  })
})

describe('dictionaries', () => {
  // The type already requires the same keys; this also catches an empty value.
  function leaves(o: object, prefix = ''): string[] {
    return Object.entries(o).flatMap(([k, v]) =>
      typeof v === 'object' && v !== null ? leaves(v, `${prefix}${k}.`) : [`${prefix}${k}`],
    )
  }
  it('have the same keys and no empty text', () => {
    expect(leaves(ja).sort()).toEqual(leaves(en).sort())
    for (const d of [ja, en]) {
      expect(Object.values(d.errors.codes).every((s) => s.length > 0)).toBe(true)
    }
  })
})

describe('switching language', () => {
  afterEach(() => setLocale('en'))

  it('changes labels and error messages', () => {
    setLocale('ja')
    expect(t.value.nav.calendar).toBe('カレンダー')
    expect(MEAL_TYPES[0].label).toBe('朝食')
    expect(errorMessage(new ApiError(401, 'invalid_credentials', ''))).toBe(
      'ユーザー名またはパスワードが正しくありません。',
    )
    setLocale('en')
    expect(MEAL_TYPES[0].label).toBe('Breakfast')
    expect(errorMessage(new ApiError(500, 'something_new', ''))).toBe('Request failed (500).')
  })

  it('formats item errors of list fields', () => {
    setLocale('ja')
    const err = new ApiError(422, 'validation_failed', '', [{ field: 'allergens[1]', reason: 'too_long' }])
    expect(fieldMessages(err)).toEqual({ allergens: '2項目め：長すぎます' })
  })
})

describe('nutrition estimate lines', () => {
  afterEach(() => setLocale('en'))

  const per100g = { calories: 144, protein_g: 8, fat_g: 4, carbs_g: 18 }

  it('say they are estimates, in both languages', () => {
    setLocale('ja')
    expect([t.value.assistant.approx, t.value.assistant.estimateNote]).toEqual(['約', '（目安）'])
    expect(t.value.assistant.estimatePer100g(per100g)).toBe('約 144 kcal / 100g · P 8g F 4g C 18g / 100g（目安）')
    setLocale('en')
    expect([t.value.assistant.approx, t.value.assistant.estimateNote]).toEqual(['About', '(estimate)'])
    expect(t.value.assistant.estimatePer100g(per100g)).toBe('About 144 kcal / 100g · P 8g F 4g C 18g / 100g (estimate)')
  })
})
