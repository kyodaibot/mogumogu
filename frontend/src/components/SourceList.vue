<script setup lang="ts">
import { computed } from 'vue'
import type { Source } from '../api'
import { t } from '../i18n'
import { sourceLinks } from '../lib/sources'

// The web pages a reply cites (plans/web-search.md), under the reply.
const props = defineProps<{ sources: Source[] }>()

const links = computed(() => sourceLinks(props.sources))
</script>

<template>
  <section v-if="links.length" class="sources" :aria-label="t.assistant.sources">
    <h4>{{ t.assistant.sources }}</h4>
    <ul>
      <li v-for="l in links" :key="l.href">
        <a :href="l.href" target="_blank" rel="noopener noreferrer">{{ l.label }}</a>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.sources {
  font-size: var(--fs-small);
  color: var(--ink-soft);
}

h4 {
  margin: 0;
  font-size: inherit;
  font-weight: 700;
}

ul {
  margin: 0.25rem 0 0;
  padding: 0;
  list-style: none;
}

li {
  padding: 0.125rem 0;
}

a {
  overflow-wrap: anywhere;
}
</style>
