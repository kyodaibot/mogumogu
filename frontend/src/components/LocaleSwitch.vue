<script setup lang="ts">
import { locale, setLocale, t, type Locale } from '../i18n'

// Two short buttons; each carries its language's own name for screen readers,
// so either can be found whatever the current language is.
const OPTIONS: { value: Locale; short: string; name: string }[] = [
  { value: 'ja', short: 'JA', name: '日本語' },
  { value: 'en', short: 'EN', name: 'English' },
]
</script>

<template>
  <div class="locale-switch" role="group" :aria-label="t.language.label">
    <button
      v-for="o in OPTIONS"
      :key="o.value"
      type="button"
      :lang="o.value"
      :aria-label="o.name"
      :aria-pressed="locale === o.value"
      @click="setLocale(o.value)"
    >
      {{ o.short }}
    </button>
  </div>
</template>

<style scoped>
.locale-switch {
  display: inline-flex;
  padding: 2px;
  border: 1px solid var(--line);
  border-radius: 999px;
  flex-shrink: 0;
}

.locale-switch button {
  border: none;
  border-radius: 999px;
  padding: 0.125rem 0.5rem;
  font-size: var(--fs-small);
  font-family: var(--font-num);
  color: var(--ink-soft);
  background: none;
}

.locale-switch button[aria-pressed='true'] {
  color: var(--ink);
  background: var(--surface);
}
</style>
