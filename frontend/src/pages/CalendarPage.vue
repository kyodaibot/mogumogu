<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type MealRecord, type MealType, type ScheduleEvent } from '../api'
import EventForm from '../components/EventForm.vue'
import MealForm from '../components/MealForm.vue'
import MealTray from '../components/MealTray.vue'
import {
  WEEKDAY_HEADERS,
  dayOfMonth,
  formatDayTitle,
  formatTime,
  monthGrid,
  overlapsDay,
  shiftMonth,
  todayTokyo,
  yearMonthOf,
  type Ymd,
} from '../lib/date'
import { MEAL_TYPES, NUTRIENTS, formatNutrient } from '../lib/meals'
import { errorMessage } from '../lib/messages'

const today = todayTokyo()
const view = ref(yearMonthOf(today))
const selected = ref<Ymd>(today)

const grid = computed(() => monthGrid(view.value.year, view.value.month))
const monthPrefix = computed(() => `${view.value.year}-${String(view.value.month).padStart(2, '0')}`)

const events = ref<ScheduleEvent[]>([])
const meals = ref<MealRecord[]>([])
const loading = ref(false)
const loadError = ref('')
const actionError = ref('')

// Which form is open in the day panel.
type Editing =
  | { kind: 'event'; item: ScheduleEvent | null }
  | { kind: 'meal'; item: MealRecord | null }
  | null
const editing = ref<Editing>(null)

let loadSeq = 0
async function load() {
  const seq = ++loadSeq
  const { start, end } = grid.value
  loading.value = true
  loadError.value = ''
  try {
    const [ev, ml] = await Promise.all([api.listEvents(start, end), api.listMeals(start, end)])
    if (seq !== loadSeq) return // a newer month was requested meanwhile
    events.value = ev
    meals.value = ml
  } catch (err) {
    if (seq === loadSeq) loadError.value = errorMessage(err)
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}
watch(grid, load, { immediate: true })

function eventsOn(day: Ymd) {
  return events.value.filter((e) => overlapsDay(e.starts_at, e.ends_at, day))
}
function mealsOn(day: Ymd) {
  return meals.value.filter((m) => m.date === day)
}

// Per-day summary for the grid cells: event count and which meal types have a record.
const summaries = computed(() => {
  const out = new Map<Ymd, { events: number; meals: MealType[] }>()
  for (const week of grid.value.weeks) {
    for (const day of week) {
      const meals = MEAL_TYPES.map((t) => t.value).filter((t) => mealsOn(day).some((m) => m.meal_type === t))
      out.set(day, { events: eventsOn(day).length, meals })
    }
  }
  return out
})

function dayLabel(day: Ymd): string {
  const s = summaries.value.get(day)!
  const parts = [formatDayTitle(day)]
  if (s.events) parts.push(`行程 ${s.events} 项`)
  parts.push(s.meals.length ? `已记录${s.meals.map((t) => MEAL_TYPES.find((x) => x.value === t)!.label).join('、')}` : '没有饮食记录')
  return parts.join('，')
}

const dayEvents = computed(() => eventsOn(selected.value))
const dayMealGroups = computed(() =>
  MEAL_TYPES.map((t) => ({ ...t, items: mealsOn(selected.value).filter((m) => m.meal_type === t.value) })).filter(
    (g) => g.items.length > 0,
  ),
)

function select(day: Ymd) {
  selected.value = day
  editing.value = null
  actionError.value = ''
  // Clicking a leading/trailing day switches to its month.
  if (!day.startsWith(monthPrefix.value)) view.value = yearMonthOf(day)
}

function goMonth(delta: number) {
  view.value = shiftMonth(view.value.year, view.value.month, delta)
  editing.value = null
}

function goToday() {
  view.value = yearMonthOf(today)
  select(today)
}

async function afterSave() {
  editing.value = null
  await load()
}

async function remove(kind: 'event' | 'meal', id: number, label: string) {
  if (!window.confirm(`确定删除「${label}」吗？`)) return
  actionError.value = ''
  try {
    if (kind === 'event') await api.deleteEvent(id)
    else await api.deleteMeal(id)
    await load()
  } catch (err) {
    actionError.value = errorMessage(err)
  }
}
</script>

<template>
  <main class="page calendar-page">
    <section class="calendar">
      <header class="month-nav">
        <h1>{{ view.year }}年{{ view.month }}月</h1>
        <span v-if="loading" class="muted loading">加载中…</span>
        <div class="month-buttons">
          <button type="button" class="icon" aria-label="上个月" @click="goMonth(-1)">‹</button>
          <button type="button" @click="goToday">今天</button>
          <button type="button" class="icon" aria-label="下个月" @click="goMonth(1)">›</button>
        </div>
      </header>

      <div v-if="loadError" class="banner error" role="alert">
        {{ loadError }}
        <button type="button" @click="load">重试</button>
      </div>

      <div class="grid" role="grid">
        <div v-for="w in WEEKDAY_HEADERS" :key="w" class="weekday" role="columnheader">{{ w }}</div>
        <template v-for="week in grid.weeks" :key="week[0]">
          <button
            v-for="day in week"
            :key="day"
            type="button"
            role="gridcell"
            class="day"
            :class="{
              outside: !day.startsWith(monthPrefix),
              today: day === today,
              selected: day === selected,
              future: day > today,
            }"
            :aria-selected="day === selected"
            :aria-label="dayLabel(day)"
            @click="select(day)"
          >
            <span class="num">{{ dayOfMonth(day) }}</span>
            <span class="marks">
              <MealTray
                v-if="(day <= today && day.startsWith(monthPrefix)) || summaries.get(day)!.meals.length"
                :filled="summaries.get(day)!.meals"
              />
              <span v-if="summaries.get(day)!.events" class="badge event">行程 {{ summaries.get(day)!.events }}</span>
            </span>
          </button>
        </template>
      </div>

      <ul class="legend" aria-label="餐盘图例">
        <li v-for="t in MEAL_TYPES" :key="t.value"><span class="swatch" :class="t.value"></span>{{ t.label }}</li>
      </ul>
    </section>

    <section class="day-panel">
      <header class="day-header">
        <div>
          <h2>{{ formatDayTitle(selected) }}</h2>
          <span v-if="selected === today" class="today-chip">今天</span>
        </div>
        <button type="button" class="ask-ai" disabled title="即将开放">Ask AI<small>即将开放</small></button>
      </header>

      <p v-if="actionError" class="banner error" role="alert">{{ actionError }}</p>

      <div class="section-title">
        <h3>行程</h3>
        <button v-if="!editing" type="button" @click="editing = { kind: 'event', item: null }">+ 新增行程</button>
      </div>
      <EventForm
        v-if="editing?.kind === 'event'"
        :key="editing.item?.id ?? 'new-event'"
        :event="editing.item"
        :date="selected"
        @saved="afterSave"
        @cancel="editing = null"
      />
      <p v-if="!loading && dayEvents.length === 0" class="muted empty">这一天还没有行程。</p>
      <ul class="items">
        <li v-for="e in dayEvents" :key="e.id" class="item event">
          <div class="body">
            <div class="time">{{ formatTime(e.starts_at, selected) }} – {{ formatTime(e.ends_at, selected) }}</div>
            <div class="title">{{ e.title }}</div>
            <div v-if="e.notes" class="notes">{{ e.notes }}</div>
          </div>
          <div class="item-actions">
            <button type="button" class="link" @click="editing = { kind: 'event', item: e }">编辑</button>
            <button type="button" class="link danger" @click="remove('event', e.id, e.title)">删除</button>
          </div>
        </li>
      </ul>

      <div class="section-title">
        <h3>饮食记录</h3>
        <button v-if="!editing" type="button" @click="editing = { kind: 'meal', item: null }">+ 新增记录</button>
      </div>
      <MealForm
        v-if="editing?.kind === 'meal'"
        :key="editing.item?.id ?? 'new-meal'"
        :meal="editing.item"
        :date="selected"
        @saved="afterSave"
        @cancel="editing = null"
      />
      <p v-if="!loading && dayMealGroups.length === 0" class="muted empty">这一天还没有饮食记录。</p>
      <div v-for="g in dayMealGroups" :key="g.value" class="meal-group" :class="g.value">
        <h4><span class="swatch" :class="g.value"></span>{{ g.label }}</h4>
        <ul class="items">
          <li v-for="m in g.items" :key="m.id" class="item">
            <div class="body">
              <div class="title">{{ m.content }}</div>
              <div class="nutrients">
                <span v-for="n in NUTRIENTS" :key="n.key" :class="{ unknown: m[n.key] === null }">
                  {{ n.label }} {{ formatNutrient(m[n.key], n.unit) }}
                </span>
              </div>
            </div>
            <div class="item-actions">
              <button type="button" class="link" @click="editing = { kind: 'meal', item: m }">编辑</button>
              <button type="button" class="link danger" @click="remove('meal', m.id, m.content)">删除</button>
            </div>
          </li>
        </ul>
      </div>
    </section>
  </main>
</template>

<style scoped>
.calendar-page {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(20rem, 26rem);
  align-items: start;
  gap: 2.5rem;
  max-width: 76rem;
}

/* On wide screens the month stays in view while a long day panel scrolls. */
.calendar {
  position: sticky;
  top: 5rem;
}

/* ---------- Month ---------- */

.month-nav {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 1rem;
}

.month-nav h1 {
  margin: 0;
  font-family: var(--font-num);
  font-size: var(--fs-h1);
  font-weight: 800;
  letter-spacing: -0.01em;
}

.month-buttons {
  display: flex;
  gap: 0.375rem;
  margin-left: auto;
}

.month-buttons button {
  padding: 0.375rem 0.875rem;
}

.month-buttons .icon {
  width: 2.375rem;
  padding: 0.375rem 0;
  font-size: 1.25rem;
  line-height: 1;
}

.grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 0.375rem;
}

.weekday {
  padding-bottom: 0.25rem;
  text-align: center;
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

.day {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  align-items: flex-start;
  min-height: 5.75rem;
  padding: 0.5rem;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--paper);
  text-align: left;
  font-weight: 400;
}

.day:hover:not(.selected) {
  border-color: var(--line-strong);
  background: var(--paper);
}

.day.outside {
  border-color: transparent;
  background: none;
}

.day.outside .num {
  color: var(--ink-soft);
  font-weight: 500;
  opacity: 0.6;
}

.day.selected {
  border-color: var(--primary);
  box-shadow: inset 0 0 0 1px var(--primary);
  background: var(--primary-soft);
}

.num {
  display: inline-grid;
  place-items: center;
  min-width: 1.75rem;
  height: 1.75rem;
  margin: -0.125rem 0 0 -0.125rem;
  border-radius: 999px;
  font-family: var(--font-num);
  font-variant-numeric: tabular-nums;
  font-weight: 700;
}

.day.today .num {
  background: var(--today);
  color: var(--on-today);
}

.marks {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 0.375rem;
}

/* A bigger tray in the roomy desktop cells; MealTray reads its slot size from --slot. */
.day :deep(.tray) {
  --slot: 10px;
}

.badge {
  padding: 0 0.375rem;
  border-radius: 6px;
  font-size: 0.6875rem;
  line-height: 1.6;
  white-space: nowrap;
}

.badge.event {
  background: var(--primary);
  color: var(--on-primary);
}

.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem 1rem;
  margin: 0.875rem 0 0;
  padding: 0;
  list-style: none;
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

.legend li {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
}

.swatch {
  width: 0.625rem;
  height: 0.625rem;
  border-radius: 3px;
}

.swatch.breakfast { background: var(--meal-breakfast); }
.swatch.lunch { background: var(--meal-lunch); }
.swatch.dinner { background: var(--meal-dinner); }
.swatch.snack { background: var(--meal-snack); }

/* ---------- Day panel ---------- */

.day-panel {
  padding: 1.5rem;
  border-radius: var(--radius-panel);
  background: var(--surface);
}

.day-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
  margin-bottom: 0.5rem;
}

.day-header h2 {
  display: inline;
  margin: 0;
  font-size: var(--fs-h2);
}

.today-chip {
  display: inline-block;
  margin-left: 0.5rem;
  padding: 0 0.5rem;
  border-radius: 999px;
  background: var(--today);
  color: var(--on-today);
  font-size: var(--fs-small);
  font-weight: 700;
  vertical-align: 0.15em;
}

.ask-ai {
  display: grid;
  justify-items: center;
  padding: 0.375rem 0.875rem;
  line-height: 1.25;
  white-space: nowrap;
  border-style: dashed;
  background: none;
}

.ask-ai small {
  font-size: 0.6875rem;
  font-weight: 400;
  color: var(--ink-soft);
}

.section-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  margin-top: 1.75rem;
}

.section-title h3 {
  margin: 0;
  font-size: var(--fs-h3);
}

.section-title button {
  padding: 0.375rem 0.75rem;
  font-size: 0.875rem;
}

.items {
  list-style: none;
  padding: 0;
  margin: 0.625rem 0 0;
  display: grid;
  gap: 0.5rem;
}

/* Each record carries its colour on the left edge: blue for events, the meal colour for meals. */
.item {
  display: flex;
  align-items: flex-start;
  gap: 0.5rem;
  padding: 0.75rem 0.5rem 0.75rem 1rem;
  border-radius: 12px;
  background: var(--paper);
  border-left: 4px solid var(--bar, var(--primary));
}

.meal-group.breakfast { --bar: var(--meal-breakfast); }
.meal-group.lunch { --bar: var(--meal-lunch); }
.meal-group.dinner { --bar: var(--meal-dinner); }
.meal-group.snack { --bar: var(--meal-snack); }

.body {
  flex: 1;
  min-width: 0;
}

.time {
  font-family: var(--font-num);
  font-variant-numeric: tabular-nums;
  font-size: var(--fs-small);
  font-weight: 600;
  color: var(--ink-soft);
}

.title {
  font-weight: 600;
  overflow-wrap: anywhere;
}

.notes {
  margin-top: 0.125rem;
  font-size: 0.875rem;
  color: var(--ink-soft);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.nutrients {
  display: flex;
  flex-wrap: wrap;
  gap: 0.125rem 0.875rem;
  margin-top: 0.125rem;
  font-family: var(--font-num);
  font-size: var(--fs-small);
}

.nutrients .unknown {
  color: var(--ink-soft);
}

.item-actions {
  display: flex;
  gap: 0.125rem;
  flex-shrink: 0;
}

.item-actions button {
  white-space: nowrap;
  font-size: 0.875rem;
}

.meal-group h4 {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin: 1rem 0 0;
  font-size: 0.875rem;
  color: var(--ink-soft);
}

.empty {
  margin: 0.625rem 0 0;
}

/* ---------- Narrow screens ---------- */

@media (max-width: 900px) {
  .calendar-page {
    grid-template-columns: minmax(0, 1fr);
    gap: 1.5rem;
    margin-top: 1.25rem;
  }

  .calendar {
    position: static;
  }

  .day-panel {
    padding: 1.25rem 1rem;
  }
}

@media (max-width: 560px) {
  .calendar-page {
    padding: 0 0.75rem;
  }

  .month-nav h1 {
    font-size: var(--fs-h2);
  }

  .grid {
    gap: 0.25rem;
  }

  .weekday {
    font-size: 0.6875rem;
  }

  .day {
    min-height: 4.25rem;
    padding: 0.25rem;
    border-radius: 9px;
  }

  .num {
    min-width: 1.5rem;
    height: 1.5rem;
    margin: 0;
    font-size: 0.875rem;
  }

  .day :deep(.tray) {
    --slot: 7px;
  }

  .badge {
    padding: 0 0.25rem;
    font-size: 0.625rem;
  }

  .day-panel {
    margin: 0 -0.75rem;
    border-radius: var(--radius-panel) var(--radius-panel) 0 0;
  }
}
</style>
