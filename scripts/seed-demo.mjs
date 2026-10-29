// Fills the demo account with two weeks of schedule events and meals, ending
// today (Asia/Tokyo), plus tomorrow's schedule. Uses the public API only, the
// same way the browser does, so it works against any environment.
//
//   read -rs DEMO_PASSWORD && export DEMO_PASSWORD
//   node scripts/seed-demo.mjs                        # https://mogumogu.teiyou416.ninja
//   SEED_URL=http://localhost:8000 node scripts/seed-demo.mjs
//
// Running it again resets the account: the profile is replaced, and every
// event and meal in the window is deleted and written again. Records outside
// the window are left alone. The password never goes into Git; pass it in
// the environment.

import { event, seedAccount, weekday } from './lib/seed.mjs'

const baseUrl = (process.env.SEED_URL ?? 'https://mogumogu.teiyou416.ninja').replace(/\/$/, '')
const username = process.env.DEMO_USERNAME ?? 'demo'
const password = process.env.DEMO_PASSWORD
if (!password || password.length < 8) {
  console.error('Set DEMO_PASSWORD (at least 8 characters).')
  process.exit(1)
}

// --- The demo person: a student who lifts and wants to build muscle ---

const profile = {
  height_cm: 170,
  weight_kg: 62,
  age: 21,
  has_exercise_habit: true,
  allergens: ['えび'],
  goal: 'build_muscle',
}

function eventsFor(date) {
  const wd = weekday(date)
  const out = []
  if (wd >= 1 && wd <= 5) out.push(event(date, '専門科目の授業', '09:00', '12:10'))
  if (wd === 2 || wd === 4) out.push(event(date, '実験', '13:00', '16:10', '実験室は飲食禁止'))
  if (wd === 1 || wd === 3 || wd === 5) out.push(event(date, 'ジム：筋トレ', '18:00', '19:30'))
  if (wd === 6) out.push(event(date, 'コンビニのアルバイト', '10:00', '16:00'))
  if (wd === 0) out.push(event(date, '友達とスーパーで買い出し', '15:00', '16:30'))
  return out
}

// --- Run ---

console.log(`Seeding ${username} at ${baseUrl}`)
const r = await seedAccount({ baseUrl, username, password, profile, eventsFor, pastDays: 14, futureDays: 1 })
console.log(`${r.registered ? 'Registered a new account' : 'Logged in to the existing account'}, ${r.from} to ${r.to}`)
console.log(`Done: replaced ${r.replaced.events} events and ${r.replaced.meals} meals with ${r.events} events and ${r.meals} meals`)
