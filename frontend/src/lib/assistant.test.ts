import { describe, expect, it } from 'vitest'
import { dateWords, mealQuestions, presetQuestions } from './assistant'

describe('assistant wording', () => {
  const today = '2026-12-31'

  it('names today, tomorrow and other dates', () => {
    expect(dateWords('2026-12-31', today)).toBe('今天')
    expect(dateWords('2027-01-01', today)).toBe('明天')
    expect(dateWords('2026-12-30', today)).toBe('12月30日')
    expect(dateWords('2027-01-05', today)).toBe('1月5日')
  })

  it('offers three meal questions and a 7-day review', () => {
    expect(mealQuestions('2027-01-05', today)).toEqual(['1月5日早餐吃什么？', '1月5日午餐吃什么？', '1月5日晚餐吃什么？'])
    expect(presetQuestions(today, today)).toEqual([
      '今天早餐吃什么？',
      '今天午餐吃什么？',
      '今天晚餐吃什么？',
      '回顾一下最近 7 天的饮食',
    ])
  })
})
