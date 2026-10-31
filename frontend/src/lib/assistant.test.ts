import { describe, expect, it } from 'vitest'
import { setLocale } from '../i18n'
import type { AssistantReply } from '../api'
import { dateWords, historyOf, mealQuestions, presetQuestions, quotaText, type SentTurn } from './assistant'

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

describe('daily quota', () => {
  it('names the user\'s own limit', () => {
    setLocale('ja')
    expect(quotaText(30)).toBe('今日の上限（30回）に達しました。また明日どうぞ。')
    setLocale('en')
    expect(quotaText(100)).toBe('You have reached today’s limit of 100 questions. Come back tomorrow.')
  })

  it('gives no number when the site\'s limit ran out', () => {
    setLocale('en')
    expect(quotaText()).toBe('You have used up today’s questions. Come back tomorrow.')
  })
})

describe('conversation history', () => {
  const estimate = { calories: 650, protein_g: 35, fat_g: 18, carbs_g: 85 }
  const reply = (text: string, ...dishes: string[]): AssistantReply => ({
    reply: text,
    recommendations: dishes.map((name) => ({
      name,
      description: `${name}の説明`,
      reason: '理由',
      estimate: { serving: estimate, per_100g: estimate },
    })),
    ask_meal_type: false,
    meal_type: dishes.length ? 'lunch' : null,
    proposal: null,
  })
  const done = (text: string, r: AssistantReply, date = '2026-10-31'): SentTurn => ({ date, text, status: 'done', reply: r })

  it('keeps answered turns in order, with only what the assistant needs', () => {
    const turns: SentTurn[] = [
      done('昼食は？', reply('3つの案です', '親子丼', 'カレー', 'うどん')),
      { date: '2026-10-31', text: '失敗した', status: 'failed' },
      done('あさっては？', reply('どの食事ですか？'), '2026-11-01'),
      { date: '2026-11-01', text: '送信中', status: 'pending' },
    ]
    expect(historyOf(turns)).toEqual([
      {
        date: '2026-10-31',
        message: '昼食は？',
        reply: '3つの案です',
        recommendations: [
          { name: '親子丼', description: '親子丼の説明' },
          { name: 'カレー', description: 'カレーの説明' },
          { name: 'うどん', description: 'うどんの説明' },
        ],
        changes: [],
      },
      { date: '2026-11-01', message: 'あさっては？', reply: 'どの食事ですか？', recommendations: [], changes: [] },
    ])
  })

  it('keeps a summary of the proposed changes', () => {
    const meal = { date: '2026-10-30', meal_type: 'dinner' as const, calories: null, protein_g: null, fat_g: null, carbs_g: null }
    const r: AssistantReply = {
      ...reply('カードで確認してください。'),
      proposal: {
        id: 1,
        expires_at: '2026-10-31T14:00:00+09:00',
        changes: [
          { op: 'update', before: { ...meal, id: 5, content: 'カレー' }, after: { ...meal, id: 5, content: 'ハヤシライス' } },
          { op: 'delete', before: { ...meal, id: 6, content: 'サラダ' }, after: null },
        ],
      },
    }
    expect(historyOf([done('昨日の夕食はハヤシライス、サラダは消して', r)])[0].changes).toEqual([
      { op: 'update', date: '2026-10-30', meal_type: 'dinner', content: 'ハヤシライス' },
      { op: 'delete', date: '2026-10-30', meal_type: 'dinner', content: 'サラダ' },
    ])
  })

  it('sends at most the last 9 turns', () => {
    const turns = Array.from({ length: 12 }, (_, i) => done(`質問${i}`, reply(`回答${i}`)))
    const history = historyOf(turns)
    expect(history).toHaveLength(9)
    expect(history[0].message).toBe('質問3')
    expect(history[8].message).toBe('質問11')
  })

  it('is empty for a new conversation', () => {
    expect(historyOf([])).toEqual([])
  })
})
