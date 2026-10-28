<script setup lang="ts">
import { reactive, ref } from 'vue'
import { api, type MealRecord, type MealType } from '../api'
import type { Ymd } from '../lib/date'
import { MEAL_TYPES, NUTRIENTS } from '../lib/meals'
import { errorMessage, fieldMessages } from '../lib/messages'
import { t } from '../i18n'

const props = defineProps<{
  /** Record to edit, or null to create one on `date`. */
  meal: MealRecord | null
  date: Ymd
}>()
const emit = defineEmits<{ saved: []; cancel: [] }>()

type NumberField = number | ''
// An empty nutrition input means "unknown" and is sent as null, never 0.
const blankOr = (v: number | null): NumberField => (v === null ? '' : v)
const nullIfBlank = (v: NumberField) => (v === '' ? null : v)

const form = reactive({
  date: props.meal?.date ?? props.date,
  meal_type: (props.meal?.meal_type ?? 'lunch') as MealType,
  content: props.meal?.content ?? '',
  calories: blankOr(props.meal?.calories ?? null),
  protein_g: blankOr(props.meal?.protein_g ?? null),
  fat_g: blankOr(props.meal?.fat_g ?? null),
  carbs_g: blankOr(props.meal?.carbs_g ?? null),
})
const saving = ref(false)
const error = ref('')
const fields = ref<Record<string, string>>({})

async function save() {
  saving.value = true
  error.value = ''
  fields.value = {}
  const body = {
    date: form.date,
    meal_type: form.meal_type,
    content: form.content,
    calories: nullIfBlank(form.calories),
    protein_g: nullIfBlank(form.protein_g),
    fat_g: nullIfBlank(form.fat_g),
    carbs_g: nullIfBlank(form.carbs_g),
  }
  try {
    if (props.meal) {
      await api.updateMeal(props.meal.id, body)
    } else {
      await api.createMeal(body)
    }
    emit('saved')
  } catch (err) {
    fields.value = fieldMessages(err)
    error.value = errorMessage(err)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <form class="card form compact" novalidate @submit.prevent="save">
    <h3>{{ meal ? t.mealForm.editTitle : t.mealForm.newTitle }}</h3>
    <div class="row">
      <label>
        {{ t.mealForm.date }}
        <input v-model="form.date" type="date" />
        <small v-if="fields.date" class="field-error">{{ fields.date }}</small>
      </label>
      <label>
        {{ t.mealForm.mealType }}
        <select v-model="form.meal_type">
          <option v-for="m in MEAL_TYPES" :key="m.value" :value="m.value">{{ m.label }}</option>
        </select>
        <small v-if="fields.meal_type" class="field-error">{{ fields.meal_type }}</small>
      </label>
    </div>
    <label>
      {{ t.mealForm.content }}
      <input v-model="form.content" maxlength="200" :placeholder="t.mealForm.contentPlaceholder" />
      <small v-if="fields.content" class="field-error">{{ fields.content }}</small>
    </label>
    <div class="row nutrients">
      <label v-for="n in NUTRIENTS" :key="n.key">
        {{ t.nutrients.withUnit(n.label, n.unit) }}
        <input v-model.number="form[n.key]" type="number" inputmode="decimal" step="0.1" min="0" :placeholder="t.nutrients.notEntered" />
        <small v-if="fields[n.key]" class="field-error">{{ fields[n.key] }}</small>
      </label>
    </div>
    <small class="hint">{{ t.mealForm.nutritionHint }}</small>
    <p v-if="error" class="banner error" role="alert">{{ error }}</p>
    <div class="actions">
      <button type="submit" class="primary" :disabled="saving">{{ saving ? t.common.saving : t.common.save }}</button>
      <button type="button" :disabled="saving" @click="emit('cancel')">{{ t.common.cancel }}</button>
    </div>
  </form>
</template>

<style scoped>
.nutrients {
  grid-template-columns: repeat(auto-fit, minmax(7rem, 1fr));
}
</style>
