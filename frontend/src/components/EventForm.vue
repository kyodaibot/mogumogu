<script setup lang="ts">
import { reactive, ref } from 'vue'
import { api, type ScheduleEvent } from '../api'
import { fromTokyoInput, toTokyoInput, type Ymd } from '../lib/date'
import { errorMessage, fieldMessages } from '../lib/messages'
import { t } from '../i18n'

const props = defineProps<{
  /** Event to edit, or null to create one on `date`. */
  event: ScheduleEvent | null
  date: Ymd
}>()
const emit = defineEmits<{ saved: []; cancel: [] }>()

// Times are entered as Tokyo wall-clock time (datetime-local has no zone).
const form = reactive({
  title: props.event?.title ?? '',
  start: props.event ? toTokyoInput(props.event.starts_at) : `${props.date}T09:00`,
  end: props.event ? toTokyoInput(props.event.ends_at) : `${props.date}T10:00`,
  notes: props.event?.notes ?? '',
})
const saving = ref(false)
const error = ref('')
const fields = ref<Record<string, string>>({})

async function save() {
  saving.value = true
  error.value = ''
  fields.value = {}
  const body = {
    title: form.title,
    starts_at: form.start ? fromTokyoInput(form.start) : '',
    ends_at: form.end ? fromTokyoInput(form.end) : '',
    notes: form.notes.trim() === '' ? null : form.notes,
  }
  try {
    if (props.event) {
      await api.updateEvent(props.event.id, body)
    } else {
      await api.createEvent(body)
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
    <h3>{{ event ? t.eventForm.editTitle : t.eventForm.newTitle }}</h3>
    <label>
      {{ t.eventForm.title }}
      <input v-model="form.title" maxlength="100" :placeholder="t.eventForm.titlePlaceholder" />
      <small v-if="fields.title" class="field-error">{{ fields.title }}</small>
    </label>
    <div class="row">
      <label>
        {{ t.eventForm.start }}
        <input v-model="form.start" type="datetime-local" />
        <small v-if="fields.starts_at" class="field-error">{{ fields.starts_at }}</small>
      </label>
      <label>
        {{ t.eventForm.end }}
        <input v-model="form.end" type="datetime-local" />
        <small v-if="fields.ends_at" class="field-error">{{ fields.ends_at }}</small>
      </label>
    </div>
    <small class="hint">{{ t.eventForm.timeHint }}</small>
    <label>
      {{ t.eventForm.notes }}
      <textarea v-model="form.notes" rows="2" maxlength="1000"></textarea>
      <small v-if="fields.notes" class="field-error">{{ fields.notes }}</small>
    </label>
    <p v-if="error" class="banner error" role="alert">{{ error }}</p>
    <div class="actions">
      <button type="submit" class="primary" :disabled="saving">{{ saving ? t.common.saving : t.common.save }}</button>
      <button type="button" :disabled="saving" @click="emit('cancel')">{{ t.common.cancel }}</button>
    </div>
  </form>
</template>
