<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, type Memory } from '../api'
import { session } from '../session'
import { errorMessage } from '../lib/messages'
import { MAX_MEMORIES, MAX_MEMORY_CHARS as MAX_CHARS, deleteMemoryIdempotent, memoryErrorText } from '../lib/memories'
import { t } from '../i18n'

// The memories on the Profile page (plans/archive/memories.md): what the meal
// assistant keeps in mind. Pro users add and edit them; every user can see
// and delete them, so a downgraded user can clear what was saved before.

const m = computed(() => t.value.memories)
const pro = computed(() => session.state.user?.tier === 'pro')

const items = ref<Memory[]>([])
const loading = ref(true)
const loadError = ref('')
const draft = ref('')
const editing = ref<{ id: number; text: string } | null>(null)
const busy = ref(false)
const error = ref('')

const full = computed(() => items.value.length >= MAX_MEMORIES)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    items.value = await api.listMemories()
  } catch (err) {
    loadError.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

/** Runs one change; false when it failed and the error is shown. */
async function change(fn: () => Promise<void>): Promise<boolean> {
  busy.value = true
  error.value = ''
  try {
    await fn()
    return true
  } catch (err) {
    error.value = memoryErrorText(err)
    return false
  } finally {
    busy.value = false
  }
}

async function add() {
  const text = draft.value.trim()
  if (!text) return
  if (await change(async () => void items.value.push(await api.createMemory(text)))) draft.value = ''
}

async function saveEdit() {
  const e = editing.value
  if (!e || !e.text.trim()) return
  await change(async () => {
    const updated = await api.updateMemory(e.id, e.text.trim())
    items.value = items.value.map((x) => (x.id === updated.id ? updated : x))
    editing.value = null
  })
}

async function remove(item: Memory) {
  if (!window.confirm(t.value.common.confirmDelete(item.content))) return
  await change(async () => {
    // Already gone (undone in the chat, for example) counts as deleted.
    await deleteMemoryIdempotent(item.id)
    items.value = items.value.filter((x) => x.id !== item.id)
  })
}

onMounted(load)
</script>

<template>
  <section id="memories" class="card memories" aria-labelledby="memories-title">
    <div class="head">
      <h2 id="memories-title">{{ m.title }}</h2>
      <span v-if="!loading && !loadError" class="count">{{ m.count(items.length, MAX_MEMORIES) }}</span>
    </div>
    <p class="lead">{{ m.lead }}</p>
    <p v-if="!pro" class="banner info">{{ m.proOnly }}</p>

    <p v-if="loading" class="muted">{{ t.common.loading }}</p>
    <div v-else-if="loadError" class="banner error" role="alert">
      {{ loadError }}
      <button type="button" @click="load">{{ t.common.retry }}</button>
    </div>
    <template v-else>
      <ul v-if="items.length" class="list">
        <li v-for="item in items" :key="item.id">
          <form v-if="editing?.id === item.id" class="row" @submit.prevent="saveEdit">
            <input v-model="editing.text" :maxlength="MAX_CHARS" :aria-label="m.title" />
            <button type="submit" class="primary" :disabled="busy || !editing.text.trim()">{{ t.common.save }}</button>
            <button type="button" :disabled="busy" @click="editing = null">{{ t.common.cancel }}</button>
          </form>
          <template v-else>
            <span class="content">{{ item.content }}</span>
            <span class="actions">
              <button
                v-if="pro"
                type="button"
                class="link"
                :disabled="busy"
                @click="editing = { id: item.id, text: item.content }"
              >
                {{ t.common.edit }}
              </button>
              <button type="button" class="link danger" :disabled="busy" @click="remove(item)">{{ t.common.delete }}</button>
            </span>
          </template>
        </li>
      </ul>
      <p v-else class="muted">{{ m.empty }}</p>

      <form v-if="pro" class="row add" @submit.prevent="add">
        <input
          v-model="draft"
          :maxlength="MAX_CHARS"
          :placeholder="m.addPlaceholder"
          :aria-label="m.addLabel"
          :disabled="full"
        />
        <button type="submit" class="primary" :disabled="busy || full || !draft.trim()">{{ m.add }}</button>
      </form>
      <small v-if="pro && full" class="muted">{{ m.full(MAX_MEMORIES) }}</small>
    </template>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
  </section>
</template>

<style scoped>
.memories {
  display: grid;
  gap: 0.75rem;
  margin-top: 1.5rem;
}

.head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.5rem;
}

h2 {
  margin: 0;
  font-size: var(--fs-h3);
}

.count {
  font-family: var(--font-num);
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

.lead {
  margin: 0;
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

.banner {
  margin: 0;
}

.list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.list li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.25rem 0.75rem;
  padding: 0.625rem 0;
  border-bottom: 1px solid var(--line);
}

.list li:first-child {
  border-top: 1px solid var(--line);
}

.content {
  flex: 1 1 12rem;
  overflow-wrap: anywhere;
}

.actions {
  display: flex;
  gap: 0.75rem;
}

.row {
  display: flex;
  flex: 1 1 100%;
  gap: 0.5rem;
}

.row input {
  flex: 1;
  min-width: 0;
}

.row button {
  flex: none;
  padding: 0.5rem 1rem;
}

.error {
  margin: 0;
  font-size: var(--fs-small);
  color: var(--bad);
}

@media (max-width: 560px) {
  .card {
    padding: 1.25rem 1rem;
  }
}
</style>
