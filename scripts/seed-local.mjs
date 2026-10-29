// Fills three local test accounts with different amounts of schedule events,
// from the past two weeks to a week ahead, so the calendar and the meal
// assistant can be tried on a busy, a normal and a light schedule. The meal
// log is the same for all three. Uses the public API only, and refuses to run
// against anything but this machine.
//
//   make seed-local                                   # reads SEED_PASSWORD from .env
//   SEED_URL=http://localhost:8000 make seed-local    # against `make app-up`
//
// Running it again resets the accounts: the profile is replaced, and every
// event and meal in the window is deleted and written again. Records outside
// the window are left alone.

import { event, seedAccount, weekday } from './lib/seed.mjs'

const baseUrl = (process.env.SEED_URL ?? 'http://localhost:5173').replace(/\/$/, '')
if (!['localhost', '127.0.0.1', '[::1]'].includes(new URL(baseUrl).hostname)) {
  console.error(`Refusing to seed ${baseUrl}: this script is for a local environment only.`)
  process.exit(1)
}
const password = process.env.SEED_PASSWORD
if (!password || password.length < 8) {
  console.error('Set SEED_PASSWORD in .env (at least 8 characters).')
  process.exit(1)
}
try {
  await fetch(`${baseUrl}/api/health/ready`)
} catch {
  console.error(`Cannot reach ${baseUrl}. Start the app first: run 'make dev' in another terminal.`)
  process.exit(1)
}

/** Whole weeks since the Unix epoch, to alternate events week by week. */
function weekNumber(date) {
  return Math.floor(Date.parse(`${date}T12:00:00Z`) / (7 * 86_400_000))
}

// An office worker with back-to-back meetings, dinners with clients, a late
// release on Fridays and plans on the weekend. Some events touch or overlap.
function busyEvents(date) {
  switch (weekday(date)) {
    case 1:
      return [
        event(date, '英会話レッスン', '07:30', '08:30'),
        event(date, '朝会', '09:00', '09:30'),
        event(date, '企画会議', '10:00', '12:00'),
        event(date, '定例会議', '13:00', '15:00'),
        event(date, '資料作成', '15:00', '18:30'),
      ]
    case 2:
    case 4:
      return [
        event(date, '朝会', '09:00', '09:30'),
        event(date, '顧客訪問', '10:00', '12:00', '移動に30分'),
        event(date, 'ランチミーティング', '12:00', '13:00'),
        event(date, '1on1', '14:30', '15:30'),
        event(date, '取引先との会食', '19:00', '21:30'),
      ]
    case 3:
      return [
        event(date, '英会話レッスン', '07:30', '08:30'),
        event(date, '朝会', '09:00', '09:30'),
        event(date, '社内研修', '10:00', '17:00'),
        event(date, 'チームミーティング', '16:30', '17:30', '研修と時間が重なる'),
      ]
    case 5:
      return [
        event(date, '朝会', '09:00', '09:30'),
        event(date, '企画会議', '10:00', '12:00'),
        event(date, '週次報告', '15:00', '16:00'),
        event(date, '夜間リリース作業', '22:00', '04:00'),
      ]
    case 6:
      return [
        event(date, 'ジム', '10:00', '11:30'),
        event(date, '買い出し', '14:00', '15:00'),
        event(date, '地元の友達と飲み会', '19:00', '23:00'),
      ]
    default:
      return [
        event(date, 'ランニング', '08:00', '09:00'),
        event(date, '資格の勉強会', '13:00', '17:00'),
      ]
  }
}

// A student with classes, labs and the gym: the same schedule as the demo.
function normalEvents(date) {
  const wd = weekday(date)
  const out = []
  if (wd >= 1 && wd <= 5) out.push(event(date, '専門科目の授業', '09:00', '12:10'))
  if (wd === 2 || wd === 4) out.push(event(date, '実験', '13:00', '16:10', '実験室は飲食禁止'))
  if (wd === 1 || wd === 3 || wd === 5) out.push(event(date, 'ジム：筋トレ', '18:00', '19:30'))
  if (wd === 6) out.push(event(date, 'コンビニのアルバイト', '10:00', '16:00'))
  if (wd === 0) out.push(event(date, '友達とスーパーで買い出し', '15:00', '16:30'))
  return out
}

// A freelancer working from home: yoga on Saturdays and a dinner with friends
// every other Wednesday.
function lightEvents(date) {
  const wd = weekday(date)
  if (wd === 6) return [event(date, 'ヨガ教室', '10:00', '11:00')]
  if (wd === 3 && weekNumber(date) % 2 === 0) return [event(date, '友人と夕食', '19:00', '21:00')]
  return []
}

const accounts = [
  {
    username: 'busy',
    profile: { height_cm: 176, weight_kg: 78, age: 34, has_exercise_habit: false, allergens: [], goal: 'lose_fat' },
    eventsFor: busyEvents,
  },
  {
    username: 'normal',
    profile: { height_cm: 170, weight_kg: 62, age: 21, has_exercise_habit: true, allergens: [], goal: 'build_muscle' },
    eventsFor: normalEvents,
  },
  {
    username: 'light',
    profile: { height_cm: 160, weight_kg: 58, age: 29, has_exercise_habit: false, allergens: ['えび'], goal: 'lose_fat' },
    eventsFor: lightEvents,
  },
]

for (const a of accounts) {
  const r = await seedAccount({ baseUrl, password, ...a, pastDays: 14, futureDays: 7 })
  console.log(
    `${a.username}: ${r.registered ? 'registered' : 'logged in'}, ${r.from} to ${r.to}, ` +
      `replaced ${r.replaced.events} events and ${r.replaced.meals} meals with ${r.events} events and ${r.meals} meals`,
  )
}
