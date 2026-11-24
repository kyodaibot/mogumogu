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
const storeIndex = ref(0)
const store = computed(() => props.card.stores[storeIndex.value]!)
const variant = ref('')
const portions = ref<Record<string, number>>({})
const editing = ref(false)
watch(store, (s) => {
  variant.value = s.items[0]?.code ?? ''
  portions.value = Object.fromEntries(s.items.map(item => [item.code, item.portions]))
  editing.value = false
}, { immediate: true })

const visibleItems = computed(() => store.value.items.filter(item => !props.card.breakfast_options || item.code === variant.value))
const draft = computed(() => mealFromMenu(props.card.menu_date, props.card.meal_type, visibleItems.value, portions.value))
const price = computed(() => {
  let total = 0
  for (const item of visibleItems.value) {
    const n = portions.value[item.code]
    if (!n) continue
    if (item.price_yen == null) return null
    total += item.price_yen * n
  }
  return total
})
const checked = computed(() => new Intl.DateTimeFormat(locale.value, {
  timeZone: 'Asia/Tokyo', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
}).format(new Date(store.value.checked_at)))
const sources = computed(() => sourceLinks(visibleItems.value.map(i => ({ title: i.name, url: i.url }))))
// A store without the day's own menu shows its latest recent one (plans/simplify-hours.md).
const shortDate = (iso: string) => iso.split('-').slice(1).map(Number).join('/')
const earlier = computed(() => store.value.menu_date === props.card.menu_date
  ? null
  : t.value.menu.earlier(shortDate(store.value.menu_date), shortDate(props.card.menu_date)))
const hoursLink = computed(() => sourceLinks([{ title: t.value.menu.hoursLink, url: store.value.hours_url }])[0])
function saved() {
  editing.value = false
  emit('saved')
}
</script>

<template>
  <h3>{{ card.name }}</h3>
  <p class="reason">{{ card.reason }}</p>

  <fieldset :disabled="editing || added" class="shops">
    <legend>{{ t.menu.store }}</legend>
    <label v-for="(s, index) in card.stores" :key="s.id">
      <input v-if="card.stores.length > 1" v-model="storeIndex" type="radio" :value="index" />
      {{ s.name }}
      <span class="campus" :class="s.campus">{{ t.campus[s.campus] }}</span>
    </label>
  </fieldset>
  <p>{{ t.menu.menuDate }} · {{ store.menu_date }}</p>
  <p v-if="earlier" class="banner info">{{ earlier }}</p>
  <p>
    <template v-if="store.hours?.length">{{ t.menu.regularHours }} {{ store.hours.join(' / ') }}</template>
    <a v-if="hoursLink" :href="hoursLink.href" target="_blank" rel="noopener noreferrer">{{ hoursLink.label }}</a>
  </p>
  <p>{{ t.menu.checked }} · {{ checked }}</p>

  <label v-if="card.breakfast_options">
    {{ t.menu.variant }}
    <select v-model="variant" :disabled="editing || added">
      <option v-for="item in store.items" :key="item.code" :value="item.code">{{ item.name }}</option>
    </select>
  </label>

  <ul class="dishes">
    <li v-for="item in visibleItems" :key="item.code">
      <strong>{{ item.name }}</strong>
      <label>
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
.shops { display: flex; flex-wrap: wrap; gap: .5rem; margin: .5rem 0; }
.shops label { display: inline-flex; align-items: center; gap: .25rem; }
.campus { padding: .05rem .5rem; white-space: nowrap; border: 1.5px solid var(--tag); border-radius: 6px; background: var(--tag-soft); color: var(--ink); font-size: var(--fs-small); }
.campus.yoshida { --tag: var(--campus-yoshida); --tag-soft: var(--campus-yoshida-soft); }
.campus.katsura { --tag: var(--campus-katsura); --tag-soft: var(--campus-katsura-soft); }
.campus.uji { --tag: var(--campus-uji); --tag-soft: var(--campus-uji-soft); }
.dishes { list-style: none; padding: 0; }
.dishes li { display: grid; grid-template-columns: 1fr auto; align-items: center; gap: .25rem; margin-block: .4rem; }
.dishes small { grid-column: 1 / -1; color: var(--ink-soft); }
.dishes input { width: 3.5rem; }
.totals { padding: .6rem; background: var(--surface); border-radius: var(--radius-control); }
dl, .sources { display: flex; flex-wrap: wrap; gap: .5rem 1rem; margin: .4rem 0; }
dt { font-size: .8rem; color: var(--ink-soft); }
dd { margin: 0; }
</style>
