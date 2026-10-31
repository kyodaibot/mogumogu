<script setup lang="ts">
import { computed } from 'vue'
import { ApiError, api, type Proposal } from '../api'
import { formatDayTitle, formatTime, type Ymd } from '../lib/date'
import { errorMessage } from '../lib/messages'
import { fieldChanges, resultText, subjectOf, type ProposalState } from '../lib/proposals'
import { t } from '../i18n'

// The card of a change proposal (plans/change-proposals.md): every change the
// assistant staged, each with a checkbox. Nothing is saved until the user
// runs it; afterwards each change shows its result and the card is locked.
const props = defineProps<{ proposal: Proposal; state: ProposalState; today: Ymd }>()
// Some records changed; the calendar reloads.
const emit = defineEmits<{ executed: [] }>()

const p = computed(() => t.value.assistant.proposal)
const locked = computed(() => !!props.state.results || props.state.closed)
const canRun = computed(() => !locked.value && !props.state.running && props.state.checked.some(Boolean))

// Errors no retry can fix.
const FINAL = ['proposal_executed', 'proposal_expired', 'pro_required', 'not_found']

async function run() {
  const s = props.state
  s.running = true
  s.error = undefined
  try {
    s.results = await api.executeProposal(
      props.proposal.id,
      s.checked.flatMap((checked, i) => (checked ? [i] : [])),
    )
    emit('executed')
  } catch (err) {
    s.error = failureText(err)
    s.closed = err instanceof ApiError && FINAL.includes(err.code)
  } finally {
    s.running = false
  }
}

function failureText(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'proposal_executed') return p.value.executed
    if (err.code === 'proposal_expired') return p.value.expired
    if (err.code === 'pro_required') return p.value.proRequired
  }
  return errorMessage(err)
}

const MARKS = { done: '✓', failed: '✗', skipped: '–' } as const
</script>

<template>
  <section class="proposal" :aria-label="p.title">
    <h3>{{ p.title }}</h3>
    <p v-if="!locked" class="hint">{{ p.hint }}</p>
    <ul>
      <li
        v-for="(c, i) in proposal.changes"
        :key="i"
        class="change"
        :class="[c.op, state.results?.[i]?.status ?? (state.checked[i] ? '' : 'unchecked')]"
      >
        <label>
          <input v-model="state.checked[i]" type="checkbox" :disabled="locked || state.running" />
          <span class="op">{{ p.ops[c.op] }}</span>
          <span class="when">{{ formatDayTitle(subjectOf(c).date) }} {{ t.meals[subjectOf(c).meal_type] }}</span>
        </label>
        <p class="content">
          <s v-if="c.op === 'delete'">{{ subjectOf(c).content }}</s>
          <template v-else>{{ subjectOf(c).content }}</template>
          <span v-if="c.op === 'create' && c.after?.calories != null" class="kcal">
            {{ t.assistant.approx }} {{ c.after.calories }} kcal{{ t.assistant.estimateNote }}
          </span>
        </p>
        <ul v-if="c.op === 'update'" class="fields">
          <li v-for="f in fieldChanges(c)" :key="f.label">
            {{ f.label }}: <span class="before">{{ f.before }}</span> → <span class="after">{{ f.after }}</span>
          </li>
        </ul>
        <p v-if="state.results?.[i]" class="result">
          {{ MARKS[state.results[i].status] }} {{ resultText(state.results[i]) }}
        </p>
      </li>
    </ul>
    <p v-if="state.error" class="error" role="alert">{{ state.error }}</p>
    <div v-if="!locked" class="actions">
      <small>{{ p.expires(formatTime(proposal.expires_at, today)) }}</small>
      <button type="button" class="primary" :disabled="!canRun" @click="run">
        {{ state.running ? p.running : p.run }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.proposal {
  padding: 0.75rem 0.875rem;
  border: 1px solid var(--line);
  border-left: 4px solid var(--primary);
  border-radius: 12px;
}

h3 {
  margin: 0;
  font-size: 1rem;
}

.hint {
  margin: 0.125rem 0 0;
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

ul {
  margin: 0;
  padding: 0;
  list-style: none;
}

.change {
  padding: 0.5rem 0;
  border-bottom: 1px solid var(--line);
}

.change:last-child {
  border-bottom: none;
}

.change.unchecked .content,
.change.skipped .content {
  opacity: 0.5;
}

label {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: var(--fs-small);
}

label input {
  width: auto;
  margin: 0;
}

.op {
  padding: 0 0.4375rem;
  border-radius: 999px;
  background: var(--primary-soft);
  color: var(--primary);
  font-weight: 700;
}

.delete .op {
  background: var(--bad-soft);
  color: var(--bad);
}

.when {
  color: var(--ink-soft);
}

.content {
  margin: 0.25rem 0 0;
  overflow-wrap: anywhere;
}

.kcal {
  margin-left: 0.5rem;
  font-size: var(--fs-small);
  color: var(--ink-soft);
  white-space: nowrap;
}

.fields {
  margin-top: 0.125rem;
  font-size: var(--fs-small);
}

.fields .before {
  color: var(--ink-soft);
}

.fields .after {
  font-weight: 600;
}

.result {
  margin: 0.25rem 0 0;
  font-size: var(--fs-small);
  font-weight: 600;
}

.done .result {
  color: var(--primary);
}

.failed .result {
  color: var(--bad);
}

.skipped .result {
  color: var(--ink-soft);
}

.error {
  margin: 0.5rem 0 0;
  font-size: var(--fs-small);
  color: var(--bad);
}

.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  margin-top: 0.5rem;
}

.actions small {
  color: var(--ink-soft);
}

.actions button {
  padding: 0.3125rem 1rem;
}
</style>
