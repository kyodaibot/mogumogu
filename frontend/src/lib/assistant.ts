// Wording for the meal assistant's chat panel (project-review 3.6).

import { addDays, type Ymd } from './date'

/** "今天", "明天" or "10月29日": how a target date reads inside a question. */
export function dateWords(date: Ymd, today: Ymd): string {
  if (date === today) return '今天'
  if (date === addDays(today, 1)) return '明天'
  const [, m, d] = date.split('-').map(Number)
  return `${m}月${d}日`
}

const MEALS = ['早餐', '午餐', '晚餐'] as const

/** The three "what should I eat" questions, one per meal. */
export function mealQuestions(date: Ymd, today: Ymd): string[] {
  return MEALS.map((meal) => `${dateWords(date, today)}${meal}吃什么？`)
}

/** Shown before the first message of a conversation. */
export function presetQuestions(date: Ymd, today: Ymd): string[] {
  return [...mealQuestions(date, today), '回顾一下最近 7 天的饮食']
}
