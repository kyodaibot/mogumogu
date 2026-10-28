import type { MealRecord, MealType } from '../api'
import { t } from '../i18n'

// Labels are getters, so they follow the current language wherever they are
// rendered.
export const MEAL_TYPES: readonly { value: MealType; readonly label: string }[] = (
  ['breakfast', 'lunch', 'dinner', 'snack'] as const
).map((value) => ({
  value,
  get label() {
    return t.value.meals[value]
  },
}))

export const NUTRIENTS = (
  [
    { key: 'calories', unit: 'kcal' },
    { key: 'protein_g', unit: 'g' },
    { key: 'fat_g', unit: 'g' },
    { key: 'carbs_g', unit: 'g' },
  ] as const satisfies readonly { key: keyof MealRecord; unit: string }[]
).map((n) => ({
  ...n,
  get label() {
    return t.value.nutrients[n.key]
  },
}))

/** "450 kcal", or "Not entered" for null. Never shows null as 0. */
export function formatNutrient(value: number | null, unit: string): string {
  return value === null ? t.value.nutrients.notEntered : `${value} ${unit}`
}
