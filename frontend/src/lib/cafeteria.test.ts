import { expect, it } from 'vitest'
import { clampPortions, mealFromMenu } from './cafeteria'

const tofu = { code: 'a', name: '豆腐', price_yen: 80, portions: 1, url: '', allergens: [{ ja: '大豆', en: 'soybean' }], nutrition: { calories: 50, protein_g: 4.1, carbs_g: 1.2, fat_g: 2 } }
const rice = { ...tofu, code: 'b', name: 'ご飯', nutrition: { calories: 200, protein_g: null, carbs_g: 40, fat_g: 0 } }

it('saves a combo as one meal, carrying unknowns and respecting removed dishes and portions', () => {
  const meal = mealFromMenu('2026-11-23', 'lunch', [tofu, rice], { a: 2, b: 1 })
  expect(meal).toEqual({ date: '2026-11-23', meal_type: 'lunch', content: '豆腐 ×2 + ご飯', calories: 300, protein_g: null, carbs_g: 42.4, fat_g: 4 })
  expect(mealFromMenu('2026-11-23', 'lunch', [tofu, rice], { a: 1, b: 0 })?.protein_g).toBe(4.1)
  expect(mealFromMenu('2026-11-23', 'breakfast', [tofu, rice], {})).toBeNull()
})

it('accepts only whole portions from 0 to 10', () => {
  expect(mealFromMenu('2026-11-23', 'lunch', [tofu], { a: 1.5 })).toBeNull()
  expect(mealFromMenu('2026-11-23', 'lunch', [tofu], { a: 11 })).toBeNull()
  expect(mealFromMenu('2026-11-23', 'lunch', [tofu], { a: -1 })).toBeNull()
  expect([1.5, 2.4, -3, 12, '' as unknown, 'x' as unknown].map(clampPortions)).toEqual([2, 2, 0, 10, 0, 0])
})
