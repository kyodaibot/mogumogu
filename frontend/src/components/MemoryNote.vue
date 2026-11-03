<script setup lang="ts">
import { computed, ref } from 'vue'
import type { MemoryUpdate } from '../api'
import { t } from '../i18n'

// The line under a reply that saved or forgot memories (plans/archive/memories.md).
// They are already saved; a new one can be undone here with ✕. The undo is
// the conversation's (lib/conversation), which keeps what was undone.
const props = defineProps<{
  updates: MemoryUpdate[]
  /** Ids of new memories already undone; kept on the turn. */
  undone: number[]
  /** Undoes one; gives the error text, or null when it is undone. */
  undo: (id: number) => Promise<string | null>
}>()

const n = computed(() => t.value.assistant.memoryNote)
const busy = ref<number | null>(null)
const error = ref('')

async function undoOne(u: MemoryUpdate) {
  busy.value = u.id
  error.value = (await props.undo(u.id)) ?? ''
  busy.value = null
}
</script>

<template>
  <details class="memory-note">
    <summary>{{ n.updated }}</summary>
    <ul>
      <li v-for="u in updates" :key="u.id" :class="[u.op, { undone: undone.includes(u.id) }]">
        <span class="op">{{ n.ops[u.op] }}</span>
        <span class="content">
          <template v-if="u.op === 'update'"><s>{{ u.before }}</s> → </template>
          <s v-if="u.op === 'delete' || undone.includes(u.id)">{{ u.content }}</s>
          <template v-else>{{ u.content }}</template>
        </span>
        <template v-if="u.op === 'create'">
          <span v-if="undone.includes(u.id)" class="done">{{ n.undone }}</span>
          <button
            v-else
            type="button"
            class="undo"
            :title="n.undo"
            :aria-label="`${n.undo}: ${u.content}`"
            :disabled="busy !== null"
            @click="undoOne(u)"
          >
            ✕
          </button>
        </template>
      </li>
    </ul>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <RouterLink to="/profile#memories" class="manage">{{ n.manage }}</RouterLink>
  </details>
</template>

<style scoped>
.memory-note {
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

summary {
  width: fit-content;
  cursor: pointer;
}

ul {
  margin: 0.375rem 0 0;
  padding: 0;
  list-style: none;
}

li {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.125rem 0;
}

.op {
  flex: none;
  font-weight: 700;
}

.content {
  flex: 1;
  color: var(--ink);
  overflow-wrap: anywhere;
}

.undone .content,
.delete .content {
  color: var(--ink-soft);
}

.undo {
  flex: none;
  width: 1.75rem;
  height: 1.75rem;
  padding: 0;
  border-radius: 999px;
  line-height: 1;
}

.done {
  flex: none;
}

.error {
  margin: 0.25rem 0 0;
  color: var(--bad);
}

.manage {
  display: inline-block;
  margin-top: 0.25rem;
}
</style>
