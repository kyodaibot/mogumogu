<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ApiError, api, type AssistantReply } from '../api'
import { mealQuestions, presetQuestions } from '../lib/assistant'
import { formatDayTitle, type Ymd } from '../lib/date'
import { errorMessage } from '../lib/messages'
import MealTray from './MealTray.vue'

// The meal assistant (project-review 3.6): a floating button that opens a
// chat panel. The conversation lives only in this component, so closing the
// panel keeps it and reloading the page starts a new one.
const props = defineProps<{ date: Ymd; today: Ymd }>()

const MAX_LENGTH = 500

interface Turn {
  id: number
  /** The target date this message was sent for. */
  date: Ymd
  text: string
  status: 'pending' | 'done' | 'failed'
  reply?: AssistantReply
  error?: string
  retryable?: boolean
}

const open = ref(false)
const turns = ref<Turn[]>([])
const draft = ref('')
const busy = computed(() => turns.value.some((t) => t.status === 'pending'))

const fab = ref<HTMLButtonElement>()
const input = ref<HTMLTextAreaElement>()
const scroller = ref<HTMLElement>()

const presets = computed(() => presetQuestions(props.date, props.today))

let nextId = 1

async function scrollToEnd() {
  await nextTick()
  scroller.value?.scrollTo({ top: scroller.value.scrollHeight })
}

async function openPanel() {
  open.value = true
  await scrollToEnd()
  input.value?.focus()
}

async function closePanel() {
  open.value = false
  await nextTick()
  fab.value?.focus()
}

/** Starts a new turn; returns false when nothing was sent. */
function send(text: string, date: Ymd = props.date): boolean {
  text = text.trim()
  if (!text || busy.value) return false
  turns.value.push({ id: nextId++, date, text, status: 'pending' })
  // Work on the reactive copy, so updates to it re-render.
  void ask(turns.value[turns.value.length - 1])
  return true
}

function sendDraft() {
  if (send(draft.value)) draft.value = ''
}

async function ask(turn: Turn) {
  turn.status = 'pending'
  turn.error = undefined
  await scrollToEnd()
  try {
    turn.reply = await api.sendAssistantMessage(turn.date, turn.text)
    turn.status = 'done'
  } catch (err) {
    turn.status = 'failed'
    turn.error = failureText(err)
    turn.retryable = !(err instanceof ApiError && ['rate_limited', 'validation_failed'].includes(err.code))
  }
  await scrollToEnd()
  input.value?.focus()
}

function failureText(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'rate_limited') return '今天的提问次数已经用完了，明天再来吧。'
    if (err.code === 'ai_unavailable') return '饮食助手这次没能回答，请重试。'
    if (err.code === 'validation_failed') return `消息最多 ${MAX_LENGTH} 个字。`
  }
  return errorMessage(err)
}

function onKeydown(e: KeyboardEvent) {
  // Enter sends, Shift+Enter starts a new line; Enter that confirms an IME
  // candidate (Chinese or Japanese input) must not send.
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && e.keyCode !== 229) {
    e.preventDefault()
    sendDraft()
  }
}

// A divider marks where the target date changes within the conversation.
function startsNewDate(i: number): boolean {
  return i > 0 && turns.value[i].date !== turns.value[i - 1].date
}

// Keep the newest message in view when the selected date changes while open.
watch(
  () => props.date,
  () => {
    if (open.value) void scrollToEnd()
  },
)
</script>

<template>
  <button
    v-show="!open"
    ref="fab"
    type="button"
    class="fab"
    aria-haspopup="dialog"
    :aria-expanded="open"
    @click="openPanel"
  >
    <MealTray :filled="['breakfast', 'lunch', 'dinner', 'snack']" size="md" />
    饮食助手
  </button>

  <section
    v-if="open"
    class="panel"
    role="dialog"
    aria-labelledby="assistant-title"
    @keydown.esc="closePanel"
  >
    <header class="panel-header">
      <div>
        <h2 id="assistant-title">饮食助手</h2>
        <p class="target">
          关于 <strong>{{ formatDayTitle(date) }}</strong><span v-if="date === today" class="today-mark">今天</span>
        </p>
      </div>
      <button type="button" class="close" aria-label="关闭饮食助手" @click="closePanel">×</button>
    </header>

    <div ref="scroller" class="messages" aria-live="polite">
      <div v-if="turns.length === 0" class="intro">
        <p>可以问我某一餐吃什么，或者让我回顾你的饮食记录。我会参考你的 Profile、行程和最近吃过的东西。</p>
        <ul class="presets" aria-label="预设问题">
          <li v-for="p in presets" :key="p">
            <button type="button" class="chip" @click="send(p)">{{ p }}</button>
          </li>
        </ul>
      </div>

      <template v-for="(t, i) in turns" :key="t.id">
        <p v-if="startsNewDate(i)" class="divider"><span>以下关于 {{ formatDayTitle(t.date) }}</span></p>

        <p class="bubble user">{{ t.text }}</p>

        <div v-if="t.status === 'pending'" class="bubble assistant thinking" role="status">
          <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>正在思考…
        </div>

        <div v-else-if="t.status === 'failed'" class="failure" role="alert">
          <span>{{ t.error }}</span>
          <button v-if="t.retryable" type="button" :disabled="busy" @click="ask(t)">重试</button>
        </div>

        <template v-else-if="t.reply">
          <p class="bubble assistant">{{ t.reply.reply }}</p>
          <ul v-if="t.reply.recommendations.length" class="recs">
            <li v-for="r in t.reply.recommendations" :key="r.name" class="rec">
              <h3>{{ r.name }}</h3>
              <p class="desc">{{ r.description }}</p>
              <p class="reason">{{ r.reason }}</p>
            </li>
          </ul>
          <ul v-if="t.reply.ask_meal_type" class="presets follow-up" aria-label="选择餐次">
            <li v-for="q in mealQuestions(t.date, today)" :key="q">
              <button type="button" class="chip" :disabled="busy" @click="send(q, t.date)">{{ q }}</button>
            </li>
          </ul>
        </template>
      </template>
    </div>

    <form class="composer" @submit.prevent="sendDraft">
      <label class="sr-only" for="assistant-input">给饮食助手的消息</label>
      <textarea
        id="assistant-input"
        ref="input"
        v-model="draft"
        rows="2"
        :maxlength="MAX_LENGTH"
        placeholder="例如：不想吃辣，只有 15 分钟"
        @keydown="onKeydown"
      ></textarea>
      <button type="submit" class="primary" :disabled="busy || !draft.trim()">发送</button>
      <small v-if="draft.length > MAX_LENGTH - 50" class="count">{{ draft.length }} / {{ MAX_LENGTH }}</small>
    </form>
  </section>
</template>

<style scoped>
.fab {
  position: fixed;
  right: 1.5rem;
  bottom: 1.5rem;
  z-index: 20;
  display: inline-flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.75rem 1.25rem 0.75rem 0.875rem;
  border: none;
  border-radius: 999px;
  background: var(--primary);
  color: var(--on-primary);
  font-size: 1rem;
  box-shadow: 0 6px 20px rgb(31 42 38 / 0.22);
}

.fab:hover:not(:disabled) {
  background: var(--primary);
  filter: brightness(1.08);
}

/* The tray mark, filled: the assistant looks at all four meals. */
.fab :deep(.tray) {
  background: rgb(255 255 255 / 0.25);
}

.panel {
  position: fixed;
  right: 1.5rem;
  bottom: 1.5rem;
  z-index: 20;
  display: flex;
  flex-direction: column;
  width: min(25rem, calc(100vw - 3rem));
  height: min(42rem, calc(100dvh - 6.5rem));
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-panel);
  background: var(--paper);
  box-shadow: 0 12px 40px rgb(31 42 38 / 0.2);
  overflow: hidden;
  animation: panel-open 0.18s ease-out;
}

@keyframes panel-open {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
}

.panel-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.875rem 0.75rem 0.75rem 1.125rem;
  border-bottom: 1px solid var(--line);
}

.panel-header h2 {
  margin: 0;
  font-size: var(--fs-h3);
}

.target {
  margin: 0.125rem 0 0;
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

.target strong {
  color: var(--ink);
  font-weight: 600;
}

.today-mark {
  margin-left: 0.375rem;
  padding: 0 0.375rem;
  border-radius: 999px;
  background: var(--today);
  color: var(--on-today);
  font-weight: 700;
}

.close {
  width: 2.25rem;
  height: 2.25rem;
  padding: 0;
  border: none;
  font-size: 1.5rem;
  line-height: 1;
  color: var(--ink-soft);
  background: none;
}

.messages {
  flex: 1;
  overflow-y: auto;
  overscroll-behavior: contain;
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
  padding: 1rem 1.125rem;
}

.intro p {
  margin: 0 0 0.875rem;
  color: var(--ink-soft);
  font-size: 0.9375rem;
}

.presets {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

.chip {
  padding: 0.4375rem 0.875rem;
  border-color: var(--primary);
  border-radius: 999px;
  color: var(--primary);
  font-weight: 500;
  font-size: 0.9375rem;
  text-align: left;
}

.chip:hover:not(:disabled) {
  background: var(--primary-soft);
}

.bubble {
  max-width: 85%;
  margin: 0;
  padding: 0.5625rem 0.875rem;
  border-radius: 14px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.bubble.user {
  align-self: flex-end;
  border-bottom-right-radius: 4px;
  background: var(--primary);
  color: var(--on-primary);
}

.bubble.assistant {
  align-self: flex-start;
  border-bottom-left-radius: 4px;
  background: var(--surface);
}

.thinking {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--ink-soft);
}

.dots {
  display: inline-flex;
  gap: 3px;
}

.dots i {
  width: 6px;
  height: 6px;
  border-radius: 2px;
  background: var(--meal-lunch);
  animation: blink 1.2s infinite;
}

.dots i:nth-child(2) {
  background: var(--meal-breakfast);
  animation-delay: 0.2s;
}

.dots i:nth-child(3) {
  background: var(--meal-dinner);
  animation-delay: 0.4s;
}

@keyframes blink {
  50% {
    opacity: 0.25;
  }
}

.recs {
  display: grid;
  gap: 0.5rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

.rec {
  padding: 0.75rem 0.875rem;
  border: 1px solid var(--line);
  border-left: 4px solid var(--meal-lunch);
  border-radius: 12px;
}

.rec h3 {
  margin: 0;
  font-size: 1rem;
}

.rec p {
  margin: 0.125rem 0 0;
  font-size: 0.875rem;
}

.rec .reason {
  color: var(--ink-soft);
}

.follow-up {
  margin-top: -0.125rem;
}

.divider {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  margin: 0.5rem 0 0;
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

.divider::before,
.divider::after {
  content: '';
  flex: 1;
  border-top: 1px solid var(--line);
}

.failure {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem 0.75rem;
  padding: 0.625rem 0.875rem;
  border-radius: var(--radius-control);
  background: var(--bad-soft);
  color: var(--bad);
  font-size: 0.9375rem;
}

.failure button {
  padding: 0.25rem 0.75rem;
  font-size: var(--fs-small);
}

.composer {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
  gap: 0.5rem;
  padding: 0.75rem;
  border-top: 1px solid var(--line);
}

.composer textarea {
  resize: none;
  min-height: 0;
}

.count {
  grid-column: 1 / -1;
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}

/* Phones: the panel takes the whole screen. */
@media (max-width: 560px) {
  .fab {
    right: 1rem;
    bottom: 1rem;
  }

  .panel {
    inset: 0;
    width: auto;
    height: 100dvh;
    border: none;
    border-radius: 0;
    box-shadow: none;
  }
}
</style>
