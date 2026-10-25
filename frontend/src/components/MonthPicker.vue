<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref } from 'vue'

// The month title as a button that opens a year + 12-month panel, so any month
// is two clicks away instead of one ‹ › press per month.
const props = defineProps<{
  year: number
  month: number
  /** Today's year and month, marked in the panel. */
  current: { year: number; month: number }
}>()
const emit = defineEmits<{ pick: [year: number, month: number] }>()

const open = ref(false)
const panelYear = ref(props.year)
const root = ref<HTMLElement>()
const trigger = ref<HTMLButtonElement>()

const MONTHS = Array.from({ length: 12 }, (_, i) => i + 1)

async function toggle() {
  if (open.value) return close()
  panelYear.value = props.year
  open.value = true
  document.addEventListener('pointerdown', onOutside)
  await nextTick()
  const months = root.value?.querySelector('.months')
  ;(months?.querySelector<HTMLButtonElement>('[aria-current="true"]') ?? months?.querySelector('button'))?.focus()
}

function close(returnFocus = false) {
  open.value = false
  document.removeEventListener('pointerdown', onOutside)
  if (returnFocus) trigger.value?.focus()
}

function onOutside(e: PointerEvent) {
  if (!root.value?.contains(e.target as Node)) close()
}

function pick(month: number) {
  emit('pick', panelYear.value, month)
  close(true)
}

onBeforeUnmount(() => document.removeEventListener('pointerdown', onOutside))
</script>

<template>
  <div ref="root" class="month-picker" @keydown.esc="close(true)">
    <button
      ref="trigger"
      type="button"
      class="trigger"
      :aria-expanded="open"
      aria-haspopup="dialog"
      aria-label="选择年份和月份"
      @click="toggle"
    >
      {{ year }}年{{ month }}月<span class="caret" aria-hidden="true">▾</span>
    </button>

    <div v-if="open" class="panel" role="dialog" aria-label="选择年份和月份">
      <div class="year-row">
        <button type="button" class="step" aria-label="上一年" @click="panelYear--">‹</button>
        <span class="year" aria-live="polite">{{ panelYear }}年</span>
        <button type="button" class="step" aria-label="下一年" @click="panelYear++">›</button>
      </div>
      <div class="months">
        <button
          v-for="m in MONTHS"
          :key="m"
          type="button"
          :class="{ shown: panelYear === year && m === month, now: panelYear === current.year && m === current.month }"
          :aria-current="panelYear === year && m === month"
          @click="pick(m)"
        >
          {{ m }}月
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.month-picker {
  position: relative;
}

.trigger {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  margin-left: -0.5rem;
  padding: 0.125rem 0.5rem;
  border-color: transparent;
  background: none;
  font: inherit;
}

.trigger:hover:not(:disabled),
.trigger[aria-expanded='true'] {
  background: var(--surface);
}

.caret {
  font-size: 0.6em;
  color: var(--ink-soft);
}

.panel {
  position: absolute;
  top: calc(100% + 0.5rem);
  left: -0.5rem;
  z-index: 5;
  width: 17rem;
  padding: 0.75rem;
  border: 1px solid var(--line);
  border-radius: var(--radius-panel);
  background: var(--paper);
  box-shadow: 0 12px 32px -12px rgb(31 42 38 / 0.35);
  font-size: var(--fs-body);
  font-weight: 400;
  letter-spacing: normal;
  animation: form-open 0.15s ease-out;
}

.year-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 0.5rem;
}

.year {
  font-family: var(--font-num);
  font-weight: 700;
}

.step {
  width: 2.25rem;
  padding: 0.25rem 0;
  border-color: transparent;
  font-size: 1.125rem;
  line-height: 1.2;
}

.months {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 0.375rem;
}

.months button {
  padding: 0.5rem 0;
  border-color: transparent;
  font-family: var(--font-num);
  font-weight: 500;
}

/* Today's month gets the "today" color as an outline; the month on screen is filled. */
.months button.now {
  border-color: var(--today);
}

.months button.shown {
  background: var(--primary);
  border-color: var(--primary);
  color: var(--on-primary);
}
</style>
