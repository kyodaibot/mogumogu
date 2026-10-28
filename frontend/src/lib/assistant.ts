// Wording for the meal assistant's chat panel (project-review 3.6). The
// questions are sent to the assistant as typed, in the current language.

import { t } from '../i18n'
import { addDays, formatShortDate, type Ymd } from './date'

/** "today", "tomorrow" or "on Oct 29": how a target date reads inside a question. */
export function dateWords(date: Ymd, today: Ymd): string {
  const a = t.value.assistant
  if (date === today) return a.today
  if (date === addDays(today, 1)) return a.tomorrow
  return a.onDate(formatShortDate(date))
}

const MEALS = ['breakfast', 'lunch', 'dinner'] as const

/** The three "what should I eat" questions, one per meal. */
export function mealQuestions(date: Ymd, today: Ymd): string[] {
  const a = t.value.assistant
  const when = dateWords(date, today)
  return MEALS.map((meal) => a.mealQuestion(when, a.mealNames[meal]))
}

/** Shown before the first message of a conversation. */
export function presetQuestions(date: Ymd, today: Ymd): string[] {
  return [...mealQuestions(date, today), t.value.assistant.reviewWeek]
}
