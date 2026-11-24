<script setup lang="ts">
import { computed } from 'vue'
import type { NutritionDay } from '../api'
import { t } from '../i18n'
import { NUTRIENTS } from '../lib/meals'

const props = defineProps<{ days: NutritionDay[] }>()
const hasMeals = computed(() => props.days.some(d => d.meals > 0))
const series = computed(() => NUTRIENTS.map(({ key, label, unit }) => {
  const values = props.days.map(d => d[key])
  const peak = Math.max(1, ...values.map(v => v ?? 0))
  const step = 10 ** Math.floor(Math.log10(peak))
  const ceiling = Math.ceil(peak / step) * step
  const points = []
  let path = ''
  for (const [i, value] of values.entries()) {
    if (value === null) continue
    const x = values.length === 1 ? 144 : 4 + i * 280 / (values.length - 1)
    const y = 54 - 48 * value / ceiling
    path += (i === 0 || values[i - 1] === null ? 'M' : 'L') + x + ',' + y
    points.push({ x, y, value, date: props.days[i]!.date })
  }
  return { key, label, unit, ceiling, points, path }
}))
</script>

<template>
  <figure v-if="days.length" class="chart">
    <figcaption>{{ t.chart.title }}</figcaption>
    <p class="range">{{ days[0]!.date }} – {{ days[days.length - 1]!.date }}</p>
    <p v-if="!hasMeals">{{ t.chart.empty }}</p>
    <template v-else>
      <div v-for="s in series" :key="s.key" class="metric" :class="s.key">
        <strong>{{ s.label }} <small>({{ s.unit }})</small></strong>
        <div class="plot">
          <div class="scale"><span>{{ s.ceiling }}</span><span>0</span></div>
          <svg viewBox="0 0 288 60" preserveAspectRatio="none" aria-hidden="true">
            <path d="M4,6H284 M4,54H284" class="grid" />
            <path :d="s.path" class="line" />
            <circle v-for="p in s.points" :key="p.date" :cx="p.x" :cy="p.y" r="2">
              <title>{{ p.date }} · {{ p.value }} {{ s.unit }}</title>
            </circle>
          </svg>
        </div>
      </div>
      <div class="axis"><span>{{ days[0]!.date.slice(5) }}</span><span v-if="days.length > 1">{{ days[days.length - 1]!.date.slice(5) }}</span></div>
      <p v-if="series.some(s => s.points.length < days.length)" class="note">{{ t.chart.gaps }}</p>
      <details>
        <summary>{{ t.chart.details }}</summary>
        <table><thead><tr><td></td><th v-for="s in series" :key="s.key" scope="col">{{ s.label }} ({{ s.unit }})</th></tr></thead>
          <tbody><tr v-for="d in days" :key="d.date"><th scope="row">{{ d.date.slice(5) }}</th>
            <td v-for="s in series" :key="s.key">{{ d[s.key] ?? '—' }}</td>
          </tr></tbody>
        </table>
      </details>
    </template>
  </figure>
</template>

<style scoped>
.chart { min-width: 0; margin: .5rem 0; padding: .8rem; background: var(--surface); border: 1px solid var(--line); border-radius: var(--radius-control); }
figcaption { font-weight: 700; }
.range, .note, .axis, .scale, small { color: var(--ink-soft); font-size: var(--fs-small); }
.range, .note { margin: .25rem 0; }
.metric { margin-top: .55rem; }
.plot { display: grid; grid-template-columns: 3em 1fr; align-items: stretch; }
.scale { display: flex; flex-direction: column; justify-content: space-between; text-align: right; padding-right: .4em; }
svg { width: 100%; height: 3.75rem; }
.calories svg { color: light-dark(#23804e, #73d89e); }
.protein_g svg { color: light-dark(#a17b00, #f3cb47); }
.fat_g svg { color: light-dark(#c33832, #ff8176); }
.carbs_g svg { color: light-dark(#be4e91, #f49acb); }
.grid, .line { fill: none; }
.grid { stroke: var(--line); }
.line { stroke: currentColor; stroke-width: 2; }
circle { fill: currentColor; }
.axis { display: flex; justify-content: space-between; margin-left: 3rem; }
details { margin-top: .6rem; overflow-x: auto; }
summary { cursor: pointer; }
table { width: 100%; margin-top: .4rem; font-size: var(--fs-small); border-collapse: collapse; }
td, th { padding: .2rem; border-bottom: 1px solid var(--line); text-align: right; }
</style>
