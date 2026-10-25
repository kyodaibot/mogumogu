<script setup lang="ts">
import type { MealType } from '../api'
import { MEAL_TYPES } from '../lib/meals'

// The 2×2 "tray": one slot per meal type (breakfast, lunch / dinner, snack),
// filled when that meal has a record. Used in the logo and the calendar cells.
withDefaults(defineProps<{ filled: readonly MealType[]; size?: 'sm' | 'md' }>(), { size: 'sm' })
</script>

<template>
  <span class="tray" :class="size" aria-hidden="true">
    <span
      v-for="t in MEAL_TYPES"
      :key="t.value"
      class="slot"
      :class="[t.value, { filled: filled.includes(t.value) }]"
    ></span>
  </span>
</template>

<style scoped>
.tray {
  display: inline-grid;
  grid-template-columns: repeat(2, var(--slot));
  gap: var(--gap);
  padding: var(--gap);
  border-radius: calc(var(--slot) / 2.5);
  background: var(--tray);
  --slot: 7px;
  --gap: 2px;
}

.tray.md {
  --slot: 11px;
  --gap: 2px;
}

.slot {
  width: var(--slot);
  height: var(--slot);
  border-radius: calc(var(--slot) / 3.5);
  background: var(--tray-slot);
}

.slot.filled.breakfast { background: var(--meal-breakfast); }
.slot.filled.lunch { background: var(--meal-lunch); }
.slot.filled.dinner { background: var(--meal-dinner); }
.slot.filled.snack { background: var(--meal-snack); }
</style>
