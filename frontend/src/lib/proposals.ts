// Change proposals in the chat (plans/change-proposals.md): what the card
// shows for each change, and what the history keeps of them.

import type { ChangeResult, HistoryChange, MealRecordInput, Proposal, ProposedChange, ProposedMeal } from '../api'
import { t } from '../i18n'
import { formatShortDate } from './date'
import { NUTRIENTS, formatNutrient } from './meals'

/** The record a change is about: the new one, or the one deleted. */
export function subjectOf(c: ProposedChange): ProposedMeal {
  return (c.after ?? c.before)!
}

/** The history keeps the kind, date, meal and content of each change. */
export function summaryOf(p: Proposal): HistoryChange[] {
  return p.changes.map((c) => {
    const { date, meal_type, content } = subjectOf(c)
    return { op: c.op, date, meal_type, content }
  })
}

export interface FieldChange {
  label: string
  before: string
  after: string
}

/** The fields an update changes, as "label: before → after". */
export function fieldChanges(c: ProposedChange): FieldChange[] {
  if (c.op !== 'update' || !c.before || !c.after) return []
  const p = t.value.assistant.proposal
  const fields: { key: keyof MealRecordInput; label: string; show: (m: ProposedMeal) => string }[] = [
    { key: 'date', label: p.fields.date, show: (m) => formatShortDate(m.date) },
    { key: 'meal_type', label: p.fields.meal_type, show: (m) => t.value.meals[m.meal_type] },
    { key: 'content', label: p.fields.content, show: (m) => m.content },
    ...NUTRIENTS.map((n) => ({ key: n.key, label: n.label, show: (m: ProposedMeal) => formatNutrient(m[n.key], n.unit) })),
  ]
  const { before, after } = c
  return fields
    .filter((f) => before[f.key] !== after[f.key])
    .map((f) => ({ label: f.label, before: f.show(before), after: f.show(after) }))
}

/** What happened to one change, once the proposal ran. */
export function resultText(r: ChangeResult): string {
  const p = t.value.assistant.proposal
  if (r.status === 'done') return p.done
  if (r.status === 'skipped') return p.skipped
  return r.reason === 'not_found' ? p.notFound : p.changedSince
}

/**
 * A proposal card's state. It lives on the chat turn, so it survives closing
 * the panel; the card changes it in place.
 */
export interface ProposalState {
  /** One per change; all checked at first. */
  checked: boolean[]
  running: boolean
  /** Set once the proposal ran; the card is then locked. */
  results?: ChangeResult[]
  error?: string
  /** The proposal can no longer run (already run, expired, not Pro). */
  closed: boolean
}

export function newProposalState(p: Proposal): ProposalState {
  return { checked: p.changes.map(() => true), running: false, closed: false }
}
