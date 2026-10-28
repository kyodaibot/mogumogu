import { describe, expect, it } from 'vitest'
import { setLocale } from '../i18n'
import { dateWords, mealQuestions, presetQuestions } from './assistant'

describe('assistant wording', () => {
  const today = '2026-12-31'

  it('names today, tomorrow and other dates in Japanese', () => {
    setLocale('ja')
    expect(dateWords('2026-12-31', today)).toBe('今日')
    expect(dateWords('2027-01-01', today)).toBe('明日')
    expect(dateWords('2027-01-05', today)).toBe('1月5日')
  })

  it('names today, tomorrow and other dates in English', () => {
    setLocale('en')
    expect(dateWords('2026-12-31', today)).toBe('today')
    expect(dateWords('2027-01-01', today)).toBe('tomorrow')
    expect(dateWords('2027-01-05', today)).toBe('on Jan 5')
  })

  it('offers three meal questions and a 7-day review in Japanese', () => {
    setLocale('ja')
    expect(mealQuestions('2027-01-05', today)).toEqual([
      '1月5日の朝食は何を食べたらいい？',
      '1月5日の昼食は何を食べたらいい？',
      '1月5日の夕食は何を食べたらいい？',
    ])
    expect(presetQuestions(today, today)).toEqual([
      '今日の朝食は何を食べたらいい？',
      '今日の昼食は何を食べたらいい？',
      '今日の夕食は何を食べたらいい？',
      '最近7日間の食事を振り返って',
    ])
  })

  it('offers the same questions in English', () => {
    setLocale('en')
    expect(presetQuestions(today, today)).toEqual([
      'What should I eat for breakfast today?',
      'What should I eat for lunch today?',
      'What should I eat for dinner today?',
      'Review my meals from the last 7 days',
    ])
    expect(mealQuestions('2027-01-05', today)[0]).toBe('What should I eat for breakfast on Jan 5?')
  })
})
