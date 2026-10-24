import type { MealRecord, MealType } from '../api'

export const MEAL_TYPES: { value: MealType; label: string }[] = [
  { value: 'breakfast', label: '早餐' },
  { value: 'lunch', label: '午餐' },
  { value: 'dinner', label: '晚餐' },
  { value: 'snack', label: '加餐' },
]

export const NUTRIENTS = [
  { key: 'calories', label: '热量', unit: 'kcal' },
  { key: 'protein_g', label: '蛋白质', unit: 'g' },
  { key: 'fat_g', label: '脂质', unit: 'g' },
  { key: 'carbs_g', label: '碳水', unit: 'g' },
] as const satisfies readonly { key: keyof MealRecord; label: string; unit: string }[]

/** "450 kcal", or "未填写" for null. Never shows null as 0. */
export function formatNutrient(value: number | null, unit: string): string {
  return value === null ? '未填写' : `${value} ${unit}`
}
