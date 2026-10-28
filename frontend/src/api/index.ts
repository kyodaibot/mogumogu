// Typed endpoints for docs/api.md, sharing one client instance.

import { createClient, readDocumentCookie, type RequestOptions } from './client'

export { ApiError, type FieldError } from './client'

export interface User {
  id: number
  username: string
  created_at: string
}

export interface Me {
  user: User
  has_profile: boolean
  csrf_token?: string
}

export type Goal = 'build_muscle' | 'lose_fat'

export interface ProfileInput {
  height_cm: number | null
  weight_kg: number | null
  age: number | null
  has_exercise_habit: boolean | null
  /** null: not filled in; []: no known allergens */
  allergens: string[] | null
  goal: Goal | null
}

export interface Profile {
  height_cm: number
  weight_kg: number
  age: number
  has_exercise_habit: boolean
  allergens: string[] | null
  goal: Goal
  updated_at: string
}

export interface ScheduleEventInput {
  title: string
  starts_at: string
  ends_at: string
  notes: string | null
}

export interface ScheduleEvent extends ScheduleEventInput {
  id: number
  created_at: string
  updated_at: string
}

export type MealType = 'breakfast' | 'lunch' | 'dinner' | 'snack'

export interface MealRecordInput {
  date: string
  meal_type: MealType
  content: string
  /** null means unknown, never 0 */
  calories: number | null
  protein_g: number | null
  fat_g: number | null
  carbs_g: number | null
}

export interface MealRecord extends MealRecordInput {
  id: number
  created_at: string
  updated_at: string
}

export interface Recommendation {
  name: string
  description: string
  reason: string
}

export interface AssistantReply {
  reply: string
  /** Empty, or exactly three. */
  recommendations: Recommendation[]
  /** The assistant asks which meal; show the three meal questions. */
  ask_meal_type: boolean
}

let unauthorizedHandler: () => void = () => {}

/** Registers what to do when the session turns out to be invalid. */
export function onUnauthorized(handler: () => void) {
  unauthorizedHandler = handler
}

const client = createClient({
  fetch: (...args) => fetch(...args),
  getCookie: readDocumentCookie,
  onUnauthorized: () => unauthorizedHandler(),
})

const req = client.request

export const api = {
  me: (opts?: RequestOptions) => req<Me>('GET', '/api/auth/me', undefined, opts),
  register: (username: string, password: string) => req<Me>('POST', '/api/auth/register', { username, password }),
  login: (username: string, password: string) => req<Me>('POST', '/api/auth/login', { username, password }),
  logout: () => req<void>('POST', '/api/auth/logout'),

  getProfile: () => req<Profile>('GET', '/api/profile'),
  putProfile: (p: ProfileInput) => req<Profile>('PUT', '/api/profile', p),

  listEvents: (from: string, to: string) =>
    req<{ items: ScheduleEvent[] }>('GET', `/api/schedule-events?from=${from}&to=${to}`).then((r) => r.items),
  createEvent: (e: ScheduleEventInput) => req<ScheduleEvent>('POST', '/api/schedule-events', e),
  updateEvent: (id: number, e: ScheduleEventInput) => req<ScheduleEvent>('PUT', `/api/schedule-events/${id}`, e),
  deleteEvent: (id: number) => req<void>('DELETE', `/api/schedule-events/${id}`),

  listMeals: (from: string, to: string) =>
    req<{ items: MealRecord[] }>('GET', `/api/meal-records?from=${from}&to=${to}`).then((r) => r.items),
  createMeal: (m: MealRecordInput) => req<MealRecord>('POST', '/api/meal-records', m),
  updateMeal: (id: number, m: MealRecordInput) => req<MealRecord>('PUT', `/api/meal-records/${id}`, m),
  deleteMeal: (id: number) => req<void>('DELETE', `/api/meal-records/${id}`),

  /** `locale` is the interface language; the reply is written in it. */
  sendAssistantMessage: (date: string, message: string, locale: 'ja' | 'en') =>
    req<AssistantReply>('POST', '/api/assistant/messages', { date, message, locale }),
}
