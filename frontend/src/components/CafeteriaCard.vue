<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { CafeteriaCard } from '../api'
import { locale, t } from '../i18n'
import { MAX_PORTIONS, clampPortions, mealFromMenu } from '../lib/cafeteria'
import { NUTRIENTS, formatNutrient } from '../lib/meals'
import { sourceLinks } from '../lib/sources'
import MealForm from './MealForm.vue'

const props = defineProps<{ card: CafeteriaCard; added: boolean }>()
const emit = defineEmits<{ saved: [] }>()
const store = computed(() => props.card.stores[0]!)
const variant = ref('')
const portions = ref<Record<string, number>>({})
const riceChoice = ref<Record<string, string>>({})
const editing = ref(false)
watch(store, (s) => {
  variant.value = s.items[0]?.code ?? ''
  riceChoice.value = Object.fromEntries(s.items.filter(i => i.rice_options?.length).map(i => [i.code, i.code]))
  portions.value = Object.fromEntries(s.items.map(item => [item.code, item.portions]))
  editing.value = false
}, { immediate: true })

const visibleItems = computed(() => store.value.items.filter(item => !props.card.breakfast_options || item.code === variant.value))
// Keep quantity controls keyed by the original recommendation even when the rice SKU changes.
const selectedItems = computed(() => visibleItems.value.flatMap(item => {
  if (!item.rice_options?.length) return [item]
  const selected = item.rice_options.find(option => option.code === riceChoice.value[item.code])
  return selected ? [{ ...selected, code: item.code }] : []
}))
const title = computed(() => store.value.items.some(i => i.rice_options?.length)
  ? selectedItems.value.map(i => i.name).join(' + ') || props.card.name
  : props.card.name)
const draft = computed(() => mealFromMenu(props.card.menu_date, props.card.meal_type, selectedItems.value, portions.value))
const price = computed(() => {
  let total = 0
  for (const item of selectedItems.value) {
    const n = portions.value[item.code]
    if (!n) continue
    if (item.price_yen == null) return null
    total += item.price_yen * n
  }
  return total
})
const sources = computed(() => sourceLinks(selectedItems.value.map(i => ({ title: i.name, url: i.url }))))
const hoursLink = computed(() => sourceLinks([{ title: t.value.menu.hoursLink, url: store.value.hours_url }])[0])
function saved() {
  editing.value = false
  emit('saved')
}
</script>

<template>
  <h3>{{ title }}</h3>
  <p class="reason">{{ card.reason }}</p>

  <p class="store">
    {{ store.name }}
    <span class="campus" :class="store.campus">{{ t.campus[store.campus] }}</span>
  </p>
  <p v-if="store.hours?.length || hoursLink">
    <template v-if="store.hours?.length">{{ t.menu.regularHours }} {{ store.hours.join(' / ') }}</template>
    <a v-if="hoursLink" :href="hoursLink.href" target="_blank" rel="noopener noreferrer">{{ locale === 'ja' ? '（' + hoursLink.label + '）' : '(' + hoursLink.label + ')' }}</a>
  </p>

  <label v-if="card.breakfast_options">
    {{ t.menu.variant }}
    <select v-model="variant" :disabled="editing || added">
      <option v-for="item in store.items" :key="item.code" :value="item.code">{{ item.name }}</option>
    </select>
  </label>

  <ul class="dishes">
    <li v-for="item in visibleItems" :key="item.code">
      <strong>{{ item.rice_options?.length ? t.menu.rice : item.name }}</strong>
      <label v-if="item.rice_options?.length" class="rice-size">
        {{ t.menu.size }}
        <select v-model="riceChoice[item.code]" :disabled="editing || added">
          <option value="">{{ t.menu.noRice }}</option>
          <option v-for="option in item.rice_options" :key="option.code" :value="option.code">{{ t.menu.riceSizes[option.size] }}</option>
        </select>
      </label>
      <label v-else>
        {{ t.menu.portions }}
        <input v-model.number="portions[item.code]" :disabled="editing || added" type="number" inputmode="numeric" min="0" :max="MAX_PORTIONS" step="1"
          @change="portions[item.code] = clampPortions(portions[item.code])" />
      </label>
      <small>{{ t.menu.allergens }} · {{ item.allergens?.map(a => a[locale]).join(' · ') || (item.allergens ? t.menu.noneListed : t.menu.unknown) }}</small>
    </li>
  </ul>

  <div v-if="draft" class="totals">
    <strong>{{ t.menu.nutrition }}</strong>
    <dl>
      <div v-for="n in NUTRIENTS" :key="n.key"><dt>{{ n.label }}</dt><dd>{{ formatNutrient(draft[n.key], n.unit) }}</dd></div>
      <div><dt>{{ t.menu.price }}</dt><dd>{{ price == null ? t.menu.unknown : '¥' + price }}</dd></div>
    </dl>
  </div>
  <p class="sources"><a v-for="s in sources" :key="s.href" :href="s.href" target="_blank" rel="noopener noreferrer">{{ s.label }}</a></p>
  <MealForm v-if="editing && draft" :date="card.menu_date" :meal="null" :draft="draft" @saved="saved" @cancel="editing = false" />
  <button v-else type="button" class="add" :disabled="added || !draft" @click="editing = true">
    {{ added ? t.assistant.added : t.assistant.addToLog }}
  </button>
</template>

<style scoped>
p, h3 { margin: 0 0 .5rem; }
.store { display: flex; flex-wrap: wrap; align-items: center; gap: .4rem; }
.campus { padding: .1em .55em; border-radius: 1em; font-size: var(--fs-small); white-space: nowrap; }
.campus.yoshida { background: var(--campus-yoshida); color: #302400; }
.campus.katsura { background: #e3f1de; color: #284923; }
.campus.uji { background: #fbe5ea; color: #643048; }
.dishes { list-style: none; padding: 0; }
.dishes li { display: grid; grid-template-columns: 1fr auto; align-items: center; gap: .25rem; margin-block: .4rem; }
.dishes small { grid-column: 1 / -1; color: var(--ink-soft); }
.dishes input { width: 3.5rem; }
.rice-size { display: inline-flex; align-items: center; gap: .3rem; font-size: var(--fs-small); }
.rice-size select { width: auto; min-height: 2rem; padding: .125rem .35rem; }
.totals { padding: .6rem; background: var(--surface); border-radius: var(--radius-control); }
dl, .sources { display: flex; flex-wrap: wrap; gap: .5rem 1rem; margin: .4rem 0; }
dt { font-size: .8rem; color: var(--ink-soft); }
dd { margin: 0; }
</style>
