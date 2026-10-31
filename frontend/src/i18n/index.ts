// The interface language: Japanese or English (plans/archive/i18n.md).
//
// The first visit follows the browser: the first of its languages that is
// Japanese or English wins, otherwise English. A choice made with the
// language switch is kept in localStorage and wins from then on.
//
// `t` is the dictionary for the current language. Templates read `t.x.y`;
// script code reads `t.value.x.y` at the moment it builds a text, so text
// built earlier (such as an error already on screen) keeps its language.

import { computed, ref } from 'vue'
import { en, type Messages } from './en'
import { ja } from './ja'

export type Locale = 'ja' | 'en'
export type { Messages }

const STORAGE_KEY = 'mogumogu.locale'
const dictionaries: Record<Locale, Messages> = { ja, en }

export function detectLocale(stored: string | null, languages: readonly string[]): Locale {
  if (stored === 'ja' || stored === 'en') return stored
  for (const lang of languages) {
    const l = lang.toLowerCase()
    if (l === 'ja' || l.startsWith('ja-')) return 'ja'
    if (l === 'en' || l.startsWith('en-')) return 'en'
  }
  return 'en'
}

// Storage can be missing or throw (private windows, blocked site data); the
// app then just uses the browser's language.
function readStored(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

function browserLanguages(): readonly string[] {
  if (typeof navigator === 'undefined') return []
  return navigator.languages?.length ? navigator.languages : [navigator.language]
}

export const locale = ref<Locale>(detectLocale(readStored(), browserLanguages()))
export const t = computed(() => dictionaries[locale.value])

function applyToDocument() {
  if (typeof document !== 'undefined') document.documentElement.lang = locale.value
}
applyToDocument()

export function setLocale(next: Locale) {
  locale.value = next
  try {
    localStorage.setItem(STORAGE_KEY, next)
  } catch {
    // Not remembered, but still switched for this page.
  }
  applyToDocument()
}
