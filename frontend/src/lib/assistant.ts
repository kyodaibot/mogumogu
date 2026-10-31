// Wording and conversation history for the meal assistant's chat panel
// (project-review 3.6). The questions are sent to the assistant as typed, in
// the current language.

import type { AssistantReply, HistoryTurn } from '../api'
import { t } from '../i18n'
import { addDays, formatShortDate, type Ymd } from './date'

/** A conversation has at most this many answered messages (project-review 3.6). */
export const MAX_TURNS = 10

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

/**
 * Shown when the daily quota is used up. limit is the user's own quota; it is
 * missing when the whole site's ran out, which says nothing about theirs.
 */
export function quotaText(limit?: number): string {
  const a = t.value.assistant
  return limit ? a.ownLimitReached(limit) : a.limitReached
}

/** What the chat keeps of a message; enough to build the history. */
export interface SentTurn {
  date: Ymd
  text: string
  status: 'pending' | 'done' | 'failed'
  reply?: AssistantReply
}

/**
 * The history sent with the next message: the answered turns so far, oldest
 * first, with only what the assistant needs to follow the conversation.
 * Failed and unanswered turns are left out.
 */
export function historyOf(turns: SentTurn[]): HistoryTurn[] {
  return turns
    .filter((turn) => turn.status === 'done' && turn.reply)
    .slice(-(MAX_TURNS - 1))
    .map((turn) => ({
      date: turn.date,
      message: turn.text,
      reply: turn.reply!.reply,
      recommendations: turn.reply!.recommendations.map(({ name, description }) => ({ name, description })),
    }))
}
