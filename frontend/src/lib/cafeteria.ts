import type { MealRecordInput, MealType, MenuItem, MenuNutrition } from '../api'

export const MAX_PORTIONS = 10

/** Whole portions 0–10; anything else typed into the field becomes the nearest valid count. */
export function clampPortions(value: unknown): number {
  const n = Number(value)
  return Number.isFinite(n) ? Math.min(MAX_PORTIONS, Math.max(0, Math.round(n))) : 0
}

/** One log entry for the actual dishes eaten. Missing official values stay unknown.
 *  Returns null when nothing is chosen or a count is not a whole number 0–10. */
export function mealFromMenu(date: string, meal_type: MealType, items: MenuItem[], portions: Record<string, number>): MealRecordInput | null {
  if (items.some(item => portions[item.code] != null && clampPortions(portions[item.code]) !== portions[item.code])) return null
  const chosen = items.filter(item => portions[item.code] > 0)
  if (!chosen.length) return null
  function total(key: keyof MenuNutrition) {
    let sum = 0
    for (const item of chosen) {
      const value = item.nutrition[key]
      if (value == null) return null
      sum += value * portions[item.code]
    }
    return Math.round(sum * 10) / 10
  }
  return {
    date, meal_type,
    content: chosen.map(item => portions[item.code] === 1 ? item.name : `${item.name} ×${portions[item.code]}`).join(' + '),
    calories: total('calories'), protein_g: total('protein_g'), fat_g: total('fat_g'), carbs_g: total('carbs_g'),
  }
}
