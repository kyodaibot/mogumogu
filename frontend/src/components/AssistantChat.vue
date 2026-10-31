<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ApiError, api, type AssistantReply, type MealRecordInput, type Recommendation } from '../api'
import { MAX_TURNS, historyOf, mealQuestions, presetQuestions, quotaText } from '../lib/assistant'
import { formatDayTitle, type Ymd } from '../lib/date'
import { errorMessage } from '../lib/messages'
import { newProposalState, type ProposalState } from '../lib/proposals'
import { locale, t } from '../i18n'
import MealForm from './MealForm.vue'
import MealTray from './MealTray.vue'
import ProposalCard from './ProposalCard.vue'

// The meal assistant (project-review 3.6): a floating button that opens a
// chat panel. The conversation lives only in this component, so closing the
// panel keeps it and reloading the page starts a new one. Every message is
// sent with the answered turns before it, so the assistant can follow up.
const props = defineProps<{ date: Ymd; today: Ymd }>()
// Meal records were added or changed from the chat; the calendar reloads.
const emit = defineEmits<{ 'meals-changed': [] }>()

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
  /** Indexes of recommendations already added to the meal log. */
  added?: number[]
  /** The state of the reply's change proposal card, if it has one. */
  proposal?: ProposalState
}

const open = ref(false)
const turns = ref<Turn[]>([])
const draft = ref('')
const busy = computed(() => turns.value.some((t) => t.status === 'pending'))
// Ten answered messages end a conversation; the user starts a new one.
const full = computed(() => turns.value.filter((t) => t.status === 'done').length >= MAX_TURNS)

const fab = ref<HTMLButtonElement>()
const input = ref<HTMLTextAreaElement>()
const scroller = ref<HTMLElement>()

const presets = computed(() => presetQuestions(props.date, props.today))

let nextId = 1

// The recommendation whose meal form is open; one at a time.
const adding = ref<{ turn: number; index: number } | null>(null)

// A recommendation becomes a new meal record on the turn's target date, with
// the serving estimate as its nutrition. The user checks it before saving.
function draftFor(turn: Turn, r: Recommendation): MealRecordInput {
  return {
    date: turn.date,
    meal_type: turn.reply?.meal_type ?? 'lunch',
    content: r.name,
    ...r.estimate.serving,
  }
}

async function openMealForm(turn: Turn, index: number) {
  adding.value = { turn: turn.id, index }
  await nextTick()
  scroller.value?.querySelector('.rec form')?.scrollIntoView({ block: 'nearest' })
}

function mealAdded(turn: Turn, index: number) {
  turn.added = [...(turn.added ?? []), index]
  adding.value = null
  emit('meals-changed')
}

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
  if (!text || busy.value || full.value) return false
  turns.value.push({ id: nextId++, date, text, status: 'pending' })
  // Work on the reactive copy, so updates to it re-render.
  void ask(turns.value[turns.value.length - 1])
  return true
}

function sendDraft() {
  if (send(draft.value)) draft.value = ''
}

async function ask(turn: Turn) {
  // Only the last turn is ever sent or retried, so the turns before it are
  // the history.
  const history = historyOf(turns.value.slice(0, turns.value.findIndex((t) => t.id === turn.id)))
  turn.status = 'pending'
  turn.error = undefined
  await scrollToEnd()
  try {
    turn.reply = await api.sendAssistantMessage(turn.date, turn.text, locale.value, history)
    if (turn.reply.proposal) turn.proposal = newProposalState(turn.reply.proposal)
    turn.status = 'done'
  } catch (err) {
    turn.status = 'failed'
    turn.error = failureText(err)
    turn.retryable = !(err instanceof ApiError && ['quota_exceeded', 'validation_failed'].includes(err.code))
  }
  await scrollToEnd()
  input.value?.focus()
}

function failureText(err: unknown): string {
  if (err instanceof ApiError) {
    const a = t.value.assistant
    if (err.code === 'quota_exceeded') return quotaText(err.limit)
    if (err.code === 'rate_limited') return a.tooFast
    if (err.code === 'ai_unavailable') return a.unavailable
    if (err.code === 'validation_failed') return a.tooLong(MAX_LENGTH)
  }
  return errorMessage(err)
}

async function newConversation() {
  turns.value = []
  adding.value = null
  await nextTick()
  input.value?.focus()
}

function onKeydown(e: KeyboardEvent) {
  // Enter sends, Shift+Enter starts a new line; Enter that confirms an IME
  // candidate (Japanese input) must not send.
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
    {{ t.assistant.name }}
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
        <h2 id="assistant-title">{{ t.assistant.name }}</h2>
        <p class="target">
          {{ t.assistant.aboutBefore }}<strong>{{ formatDayTitle(date) }}</strong>{{ t.assistant.aboutAfter
          }}<span v-if="date === today" class="today-mark">{{ t.common.today }}</span>
        </p>
      </div>
      <div class="header-actions">
        <button v-if="turns.length" type="button" class="new" :disabled="busy" @click="newConversation">
          {{ t.assistant.newConversation }}
        </button>
        <button type="button" class="close" :aria-label="t.assistant.close" @click="closePanel">×</button>
      </div>
    </header>

    <div ref="scroller" class="messages" aria-live="polite">
      <div v-if="turns.length === 0" class="intro">
        <p>{{ t.assistant.intro }}</p>
        <ul class="presets" :aria-label="t.assistant.presets">
          <li v-for="p in presets" :key="p">
            <button type="button" class="chip" @click="send(p)">{{ p }}</button>
          </li>
        </ul>
      </div>

      <template v-for="(turn, i) in turns" :key="turn.id">
        <p v-if="startsNewDate(i)" class="divider"><span>{{ t.assistant.dateDivider(formatDayTitle(turn.date)) }}</span></p>

        <p class="bubble user">{{ turn.text }}</p>

        <div v-if="turn.status === 'pending'" class="bubble assistant thinking" role="status">
          <span class="dots" aria-hidden="true"><i></i><i></i><i></i></span>{{ t.assistant.thinking }}
        </div>

        <div v-else-if="turn.status === 'failed'" class="failure" role="alert">
          <span>{{ turn.error }}</span>
          <!-- Retrying an older turn would put it after later ones in the history. -->
          <button v-if="turn.retryable && i === turns.length - 1" type="button" :disabled="busy" @click="ask(turn)">
            {{ t.common.retry }}
          </button>
        </div>

        <template v-else-if="turn.reply">
          <p class="bubble assistant">{{ turn.reply.reply }}</p>
          <ul v-if="turn.reply.recommendations.length" class="recs">
            <li v-for="(r, i) in turn.reply.recommendations" :key="r.name" class="rec">
              <h3>{{ r.name }}</h3>
              <p class="desc">{{ r.description }}</p>
              <p class="reason">{{ r.reason }}</p>
              <div class="estimate">
                <p class="serving">
                  <span class="kcal">
                    {{ t.assistant.approx }} <b>{{ r.estimate.serving.calories }}</b> kcal<span class="note">{{ t.assistant.estimateNote }}</span>
                  </span>
                  <span class="macros">
                    <span class="macro p">P {{ r.estimate.serving.protein_g }}g</span>
                    <span class="macro f">F {{ r.estimate.serving.fat_g }}g</span>
                    <span class="macro c">C {{ r.estimate.serving.carbs_g }}g</span>
                  </span>
                </p>
                <p class="per100">{{ t.assistant.estimatePer100g(r.estimate.per_100g) }}</p>
              </div>
              <MealForm
                v-if="adding?.turn === turn.id && adding.index === i"
                :meal="null"
                :date="turn.date"
                :draft="draftFor(turn, r)"
                @saved="mealAdded(turn, i)"
                @cancel="adding = null"
              />
              <button
                v-else-if="turn.reply.meal_type"
                type="button"
                class="add"
                :disabled="turn.added?.includes(i)"
                @click="openMealForm(turn, i)"
              >
                {{ turn.added?.includes(i) ? t.assistant.added : t.assistant.addToLog }}
              </button>
            </li>
          </ul>
          <ProposalCard
            v-if="turn.reply.proposal && turn.proposal"
            :proposal="turn.reply.proposal"
            :state="turn.proposal"
            :today="today"
            @executed="emit('meals-changed')"
          />
          <ul v-if="turn.reply.ask_meal_type" class="presets follow-up" :aria-label="t.assistant.chooseMeal">
            <li v-for="q in mealQuestions(turn.date, today)" :key="q">
              <button type="button" class="chip" :disabled="busy || full" @click="send(q, turn.date)">{{ q }}</button>
            </li>
          </ul>
        </template>
      </template>

      <div v-if="full" class="limit" role="status">
        <span>{{ t.assistant.turnLimit(MAX_TURNS) }}</span>
        <button type="button" class="primary" @click="newConversation">{{ t.assistant.newConversation }}</button>
      </div>
    </div>

    <form class="composer" @submit.prevent="sendDraft">
      <label class="sr-only" for="assistant-input">{{ t.assistant.inputLabel }}</label>
      <textarea
        id="assistant-input"
        ref="input"
        v-model="draft"
        rows="2"
        :maxlength="MAX_LENGTH"
        :placeholder="t.assistant.inputPlaceholder"
        :disabled="full"
        @keydown="onKeydown"
      ></textarea>
      <button type="submit" class="primary" :disabled="busy || full || !draft.trim()">{{ t.assistant.send }}</button>
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

.header-actions {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}

.new {
  padding: 0.25rem 0.75rem;
  font-size: var(--fs-small);
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

/* The serving estimate stands out; per 100 g is a quiet reference line. */
.estimate {
  margin-top: 0.625rem;
  font-variant-numeric: tabular-nums;
}

.rec .serving {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.25rem 0.625rem;
  margin: 0;
}

/* Kcal keeps its note; the three tags wrap together, never one by one. */
.kcal {
  font-size: 0.875rem;
  white-space: nowrap;
}

.macros {
  display: inline-flex;
  gap: 0.25rem;
}

.kcal b {
  font-family: var(--font-num);
  font-size: 1.375rem;
  font-weight: 700;
  line-height: 1;
}

.macro {
  padding: 0.0625rem 0.4375rem;
  border-radius: 999px;
  font-family: var(--font-num);
  font-size: 0.8125rem;
  font-weight: 600;
}

.macro.p {
  background: var(--macro-p-soft);
  color: var(--macro-p);
}

.macro.f {
  background: var(--macro-f-soft);
  color: var(--macro-f);
}

.macro.c {
  background: var(--macro-c-soft);
  color: var(--macro-c);
}

.note {
  margin-left: 0.125rem;
  font-size: 0.75rem;
  color: var(--ink-soft);
}

.rec .per100 {
  margin-top: 0.25rem;
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

.rec .add {
  margin-top: 0.5rem;
  padding: 0.25rem 0.75rem;
  font-size: var(--fs-small);
}

.rec .add:disabled {
  border-color: transparent;
  color: var(--primary);
  opacity: 1;
}

/* The meal form opens inside the card: a divider, not a second card. */
.rec form {
  margin: 0.75rem 0 0;
  padding: 0.75rem 0 0;
  border: none;
  border-top: 1px solid var(--line);
  border-radius: 0;
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

.limit {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem 0.75rem;
  padding: 0.625rem 0.875rem;
  border-radius: var(--radius-control);
  background: var(--surface);
  font-size: 0.9375rem;
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
