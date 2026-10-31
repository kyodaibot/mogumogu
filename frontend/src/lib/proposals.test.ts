import { describe, expect, it } from 'vitest'
import type { ProposedChange, ProposedMeal } from '../api'
import { setLocale } from '../i18n'
import { fieldChanges, newProposalState, resultText, subjectOf } from './proposals'

const curry: ProposedMeal = {
  id: 5, date: '2026-10-30', meal_type: 'dinner', content: 'カレー',
  calories: 700, protein_g: 20, fat_g: 25, carbs_g: 95,
}

describe('proposed changes', () => {
  it('shows the new record, or the one deleted', () => {
    const ramen = { ...curry, id: undefined, content: 'ラーメン' }
    expect(subjectOf({ op: 'create', before: null, after: ramen }).content).toBe('ラーメン')
    expect(subjectOf({ op: 'delete', before: curry, after: null }).content).toBe('カレー')
  })

  it('lists only the fields an update changes', () => {
    const update: ProposedChange = {
      op: 'update',
      before: curry,
      after: { ...curry, meal_type: 'lunch', content: 'ハヤシライス', calories: null, protein_g: null, fat_g: null, carbs_g: null },
    }
    setLocale('ja')
    expect(fieldChanges(update).map((f) => `${f.label}: ${f.before} → ${f.after}`)).toEqual([
      '食事: 夕食 → 昼食',
      '内容: カレー → ハヤシライス',
      'エネルギー: 700 kcal → 未入力',
      'たんぱく質: 20 g → 未入力',
      '脂質: 25 g → 未入力',
      '炭水化物: 95 g → 未入力',
    ])
    setLocale('en')
    const moved = { ...update, after: { ...curry, date: '2026-10-29' } }
    expect(fieldChanges(moved)).toEqual([{ label: 'Date', before: 'Oct 30', after: 'Oct 29' }])
    expect(fieldChanges({ op: 'delete', before: curry, after: null })).toEqual([])
  })

  it('says what happened to each change', () => {
    setLocale('en')
    expect(resultText({ status: 'done', meal_id: 5 })).toBe('Done')
    expect(resultText({ status: 'skipped' })).toBe('Skipped')
    expect(resultText({ status: 'failed', reason: 'changed' })).toMatch(/edited after/)
    expect(resultText({ status: 'failed', reason: 'not_found' })).toMatch(/no longer exists/)
  })

  it('starts with every change checked', () => {
    const state = newProposalState({ id: 1, expires_at: '', changes: [{ op: 'delete', before: curry, after: null }, { op: 'delete', before: curry, after: null }] })
    expect(state).toEqual({ checked: [true, true], running: false, closed: false })
  })
})
