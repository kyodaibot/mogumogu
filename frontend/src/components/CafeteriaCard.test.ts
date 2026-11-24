import { h } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { expect, it } from 'vitest'
import type { CafeteriaCard } from '../api'
import Card from './CafeteriaCard.vue'

it('identifies the first cafeteria without offering a store selector', async () => {
  const stores: CafeteriaCard['stores'] = ['中央食堂', '北部食堂'].map((name, index) => ({
    id: String(index), name, campus: 'yoshida', menu_date: '2026-11-24',
    hours: [], hours_url: '', checked_at: '2026-11-24T01:00:00Z',
    items: [], nutrition: null, price_yen: null,
  }))
  const card: CafeteriaCard = {
    kind: 'cafeteria', name: '豆腐', reason: '', meal_type: 'lunch',
    menu_date: '2026-11-24', breakfast_options: false, stores,
  }
  const html = await renderToString(h(Card, { card, added: false }))
  expect(html).toContain('中央食堂')
  expect(html).not.toContain('type="radio"')
  expect(html).not.toContain('北部食堂')
})
