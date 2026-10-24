<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type MealRecord, type ScheduleEvent } from '../api'
import EventForm from '../components/EventForm.vue'
import MealForm from '../components/MealForm.vue'
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
        <button type="button" aria-label="上个月" @click="goMonth(-1)">‹</button>
        <h1>{{ view.year }}年{{ view.month }}月</h1>
        <button type="button" aria-label="下个月" @click="goMonth(1)">›</button>
        <button type="button" @click="goToday">今天</button>
        <span v-if="loading" class="muted">加载中…</span>
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
            }"
            :aria-selected="day === selected"
            :aria-label="formatDayTitle(day)"
            @click="select(day)"
          >
            <span class="num">{{ dayOfMonth(day) }}</span>
            <span v-if="eventsOn(day).length" class="badge event">行程 {{ eventsOn(day).length }}</span>
            <span v-if="mealsOn(day).length" class="badge meal">饮食 {{ mealsOn(day).length }}</span>
          </button>
        </template>
      </div>
    </section>

    <section class="day-panel">
      <header class="day-header">
        <h2>{{ formatDayTitle(selected) }}<span v-if="selected === today" class="muted">（今天）</span></h2>
        <button type="button" class="ask-ai" disabled title="即将开放">Ask AI（即将开放）</button>
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
        <li v-for="e in dayEvents" :key="e.id" class="item">
          <div>
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
      <div v-for="g in dayMealGroups" :key="g.value" class="meal-group">
        <h4>{{ g.label }}</h4>
        <ul class="items">
          <li v-for="m in g.items" :key="m.id" class="item">
            <div>
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
  grid-template-columns: minmax(0, 3fr) minmax(18rem, 2fr);
  gap: 1.5rem;
  max-width: 72rem;
}

@media (max-width: 800px) {
  .calendar-page {
    grid-template-columns: 1fr;
  }
}

.month-nav {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
}

.month-nav h1 {
  font-size: 1.3rem;
  margin: 0 0.5rem;
  min-width: 7.5rem;
  text-align: center;
}

.grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 2px;
}

.weekday {
  text-align: center;
  font-size: 0.8rem;
  color: var(--muted);
  padding: 0.25rem 0;
}

.day {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  min-height: 4.5rem;
  padding: 0.3rem;
  border-radius: 4px;
  text-align: left;
}

.day.outside {
  opacity: 0.45;
}

.day.today .num {
  background: var(--accent);
  color: var(--bg);
  border-radius: 999px;
  padding: 0 0.4rem;
}

.day.selected {
  border-color: var(--accent);
  box-shadow: inset 0 0 0 1px var(--accent);
}

.num {
  font-weight: 600;
  font-size: 0.9rem;
}

.badge {
  font-size: 0.7rem;
  border-radius: 3px;
  padding: 0 0.3rem;
  white-space: nowrap;
}

.badge.event {
  background: var(--event-bg);
}

.badge.meal {
  background: var(--meal-bg);
}

.day-header,
.section-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.day-header h2 {
  font-size: 1.15rem;
  margin: 0.25rem 0;
}

.section-title {
  margin-top: 1.25rem;
}

.section-title h3 {
  margin: 0;
  font-size: 1rem;
}

.ask-ai {
  white-space: nowrap;
}

.items {
  list-style: none;
  padding: 0;
  margin: 0.5rem 0;
  display: grid;
  gap: 0.5rem;
}

.item {
  display: flex;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.5rem 0.75rem;
  border: 1px solid var(--border);
  border-radius: 6px;
}

.time {
  font-size: 0.85rem;
  color: var(--muted);
}

.title {
  font-weight: 600;
}

.notes {
  font-size: 0.85rem;
  white-space: pre-wrap;
}

.nutrients {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem 0.75rem;
  font-size: 0.8rem;
}

.nutrients .unknown {
  color: var(--muted);
}

.item-actions {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.25rem;
}

.meal-group h4 {
  margin: 0.75rem 0 0;
  font-size: 0.9rem;
  color: var(--muted);
}

.empty {
  margin: 0.5rem 0;
}
</style>
