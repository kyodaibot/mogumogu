<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError, type Goal, type Profile } from '../api'
import { session } from '../session'
import { errorMessage, fieldMessages } from '../lib/messages'
import { t } from '../i18n'

const router = useRouter()

// Number inputs give '' when empty; that is sent as null so the API reports "required".
type NumberField = number | ''

const form = reactive({
  height_cm: '' as NumberField,
  weight_kg: '' as NumberField,
  age: '' as NumberField,
  has_exercise_habit: null as boolean | null,
  // Three distinct states, matching the API's null / [] / list.
  allergenMode: 'unset' as 'unset' | 'none' | 'list',
  allergenText: '',
  goal: null as Goal | null,
})

const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const saveError = ref('')
const saved = ref(false)
const fields = ref<Record<string, string>>({})
const isNew = ref(false)

function fill(p: Profile) {
  form.height_cm = p.height_cm
  form.weight_kg = p.weight_kg
  form.age = p.age
  form.has_exercise_habit = p.has_exercise_habit
  form.goal = p.goal
  if (p.allergens === null) {
    form.allergenMode = 'unset'
  } else if (p.allergens.length === 0) {
    form.allergenMode = 'none'
  } else {
    form.allergenMode = 'list'
    form.allergenText = p.allergens.join(t.value.profile.allergenJoiner)
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    fill(await api.getProfile())
  } catch (err) {
    if (err instanceof ApiError && err.code === 'profile_not_found') {
      isNew.value = true
    } else {
      loadError.value = errorMessage(err)
    }
  } finally {
    loading.value = false
  }
}

/** Splits on ASCII or full-width commas, the Japanese 、 and newlines. */
function parseAllergens(text: string): string[] {
  return text.split(/[,，、\n]/).map((s) => s.trim()).filter((s) => s !== '')
}

function allergens(): string[] | null {
  switch (form.allergenMode) {
    case 'unset':
      return null
    case 'none':
      return []
    case 'list':
      return parseAllergens(form.allergenText)
  }
}

const num = (v: NumberField) => (v === '' ? null : v)

async function save() {
  saving.value = true
  saved.value = false
  saveError.value = ''
  fields.value = {}

  if (form.allergenMode === 'list' && parseAllergens(form.allergenText).length === 0) {
    fields.value = { allergens: t.value.profile.allergensEmpty }
    saving.value = false
    return
  }
  try {
    fill(
      await api.putProfile({
        height_cm: num(form.height_cm),
        weight_kg: num(form.weight_kg),
        age: num(form.age),
        has_exercise_habit: form.has_exercise_habit,
        allergens: allergens(),
        goal: form.goal,
      }),
    )
    session.markProfileSaved()
    if (isNew.value) {
      await router.push('/')
      return
    }
    saved.value = true
  } catch (err) {
    fields.value = fieldMessages(err)
    saveError.value = errorMessage(err)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <main class="page narrow profile">
    <h1>{{ t.nav.profile }}</h1>
    <p class="lead">{{ t.profile.lead }}</p>
    <p v-if="isNew" class="banner info">{{ t.profile.welcome }}</p>

    <p v-if="loading" class="muted">{{ t.common.loading }}</p>
    <div v-else-if="loadError" class="banner error" role="alert">
      {{ loadError }}
      <button type="button" @click="load">{{ t.common.retry }}</button>
    </div>

    <form v-else class="card form" novalidate @submit.prevent="save">
      <section class="group">
        <h2>{{ t.profile.body }}</h2>
        <div class="row body-row">
          <label>
            {{ t.profile.height }}
            <input v-model.number="form.height_cm" type="number" inputmode="decimal" step="0.1" min="50" max="300" />
            <small v-if="fields.height_cm" class="field-error">{{ fields.height_cm }}</small>
          </label>
          <label>
            {{ t.profile.weight }}
            <input v-model.number="form.weight_kg" type="number" inputmode="decimal" step="0.1" min="10" max="500" />
            <small v-if="fields.weight_kg" class="field-error">{{ fields.weight_kg }}</small>
          </label>
          <label>
            {{ t.profile.age }}
            <input v-model.number="form.age" type="number" inputmode="numeric" step="1" min="10" max="120" />
            <small v-if="fields.age" class="field-error">{{ fields.age }}</small>
          </label>
        </div>
      </section>

      <section class="group">
        <h2>{{ t.profile.habitsAndGoal }}</h2>
        <fieldset>
          <legend>{{ t.profile.exerciseHabit }}</legend>
          <label class="inline"><input v-model="form.has_exercise_habit" type="radio" :value="true" /> {{ t.profile.yes }}</label>
          <label class="inline"><input v-model="form.has_exercise_habit" type="radio" :value="false" /> {{ t.profile.no }}</label>
          <small v-if="fields.has_exercise_habit" class="field-error">{{ fields.has_exercise_habit }}</small>
        </fieldset>

        <fieldset>
          <legend>{{ t.profile.goal }}</legend>
          <label class="inline"><input v-model="form.goal" type="radio" value="build_muscle" /> {{ t.profile.buildMuscle }}</label>
          <label class="inline"><input v-model="form.goal" type="radio" value="lose_fat" /> {{ t.profile.loseFat }}</label>
          <small v-if="fields.goal" class="field-error">{{ fields.goal }}</small>
        </fieldset>
      </section>

      <section class="group">
        <h2>{{ t.profile.allergens }}</h2>
        <fieldset>
          <legend class="visually-hidden">{{ t.profile.allergens }}</legend>
          <label class="inline"><input v-model="form.allergenMode" type="radio" value="unset" /> {{ t.profile.allergensUnset }}</label>
          <label class="inline"><input v-model="form.allergenMode" type="radio" value="none" /> {{ t.profile.allergensNone }}</label>
          <label class="inline"><input v-model="form.allergenMode" type="radio" value="list" /> {{ t.profile.allergensList }}</label>
          <label v-if="form.allergenMode === 'list'" class="allergen-input">
            <input v-model="form.allergenText" :placeholder="t.profile.allergensPlaceholder" :aria-label="t.profile.allergensListLabel" />
            <small class="hint">{{ t.profile.allergensHint }}</small>
          </label>
          <small v-if="fields.allergens" class="field-error">{{ fields.allergens }}</small>
        </fieldset>
      </section>

      <p v-if="saveError" class="banner error" role="alert">{{ saveError }}</p>
      <p v-if="saved" class="banner success" role="status">{{ t.profile.saved }}</p>

      <button type="submit" class="primary submit" :disabled="saving">
        {{ saving ? t.common.saving : isNew ? t.profile.saveAndStart : t.common.save }}
      </button>
    </form>
  </main>
</template>

<style scoped>
.profile h1 {
  margin: 0 0 0.375rem;
}

.lead {
  margin: 0 0 1.5rem;
  color: var(--ink-soft);
}

.form {
  gap: 0;
}

.group {
  display: grid;
  gap: 1.25rem;
  padding-bottom: 1.5rem;
  margin-bottom: 1.5rem;
  border-bottom: 1px solid var(--line);
}

.group h2 {
  margin: 0;
  font-size: var(--fs-h3);
}

/* Three short numbers side by side, also on phones. */
.body-row {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.allergen-input {
  margin-top: 0.25rem;
}

.visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}

.form > .banner {
  margin: 0 0 1rem;
}

.submit {
  justify-self: start;
  padding: 0.75rem 1.75rem;
}

@media (max-width: 560px) {
  .card {
    padding: 1.25rem 1rem;
  }

  .body-row {
    gap: 0.5rem;
  }

  .submit {
    justify-self: stretch;
  }
}
</style>
