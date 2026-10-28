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

const baseUrl = (process.env.SEED_URL ?? 'https://mogumogu.teiyou416.ninja').replace(/\/$/, '')
const username = process.env.DEMO_USERNAME ?? 'demo'
const password = process.env.DEMO_PASSWORD
if (!password || password.length < 8) {
  console.error('Set DEMO_PASSWORD (at least 8 characters).')
  process.exit(1)
}

const DAYS = 14
const ZONE = '+09:00'

// --- A tiny client: cookies, CSRF header and Origin, like the frontend ---

const cookies = new Map()

async function api(method, path, body) {
  const headers = { Origin: new URL(baseUrl).origin }
  if (cookies.size) headers.Cookie = [...cookies].map(([k, v]) => `${k}=${v}`).join('; ')
  if (cookies.has('mogumogu_csrf')) headers['X-CSRF-Token'] = cookies.get('mogumogu_csrf')
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(baseUrl + path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  for (const c of res.headers.getSetCookie()) {
    const [pair] = c.split(';')
    const i = pair.indexOf('=')
    cookies.set(pair.slice(0, i), pair.slice(i + 1))
  }
  const text = await res.text()
  return { status: res.status, data: text ? JSON.parse(text) : null }
}

async function must(method, path, body, ok = [200, 201, 204]) {
  const r = await api(method, path, body)
  if (!ok.includes(r.status)) {
    throw new Error(`${method} ${path} -> ${r.status} ${JSON.stringify(r.data?.error ?? r.data)}`)
  }
  return r.data
}

// --- Dates in the product time zone ---

function tokyoDate(offsetDays) {
  const d = new Date(Date.now() + offsetDays * 86_400_000)
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Tokyo' }).format(d) // YYYY-MM-DD
}

function weekday(date) {
  return new Date(`${date}T12:00:00${ZONE}`).getUTCDay() // 0 = Sunday
}

// --- The demo person: a student who lifts and wants to build muscle ---

const profile = {
  height_cm: 170,
  weight_kg: 62,
  age: 21,
  has_exercise_habit: true,
  allergens: ['虾'],
  goal: 'build_muscle',
}

const breakfasts = [
  ['燕麦牛奶配香蕉', 420, 15, 9, 70],
  ['全麦吐司和两个煮鸡蛋', 380, 20, 14, 40],
  ['饭团和味噌汤', 350, 9, 4, 68],
  ['希腊酸奶配格兰诺拉', 400, 22, 10, 52],
]
const lunches = [
  ['学校食堂的鸡肉盖饭', 750, 32, 20, 105],
  ['便利店沙拉鸡胸和饭团', 520, 34, 9, 72],
  ['豚骨拉面', 820, 25, 32, 100],
  ['烤鲑鱼定食', 680, 35, 18, 85],
  ['咖喱饭', 780, 18, 24, 120],
]
const dinners = [
  ['自己做的番茄炒蛋和米饭', 650, 22, 20, 90],
  ['牛肉乌冬面', 600, 26, 14, 88],
  ['烤鱼和蔬菜', 550, 38, 18, 40],
  ['麻婆豆腐盖饭', 720, 28, 26, 92],
  ['居酒屋聚餐', null, null, null, null],
]

function meal(date, type, [content, calories, protein, fat, carbs], withNutrition) {
  return withNutrition
    ? { date, meal_type: type, content, calories, protein_g: protein, fat_g: fat, carbs_g: carbs }
    : { date, meal_type: type, content }
}

// Meals for the past days and today, with gaps on purpose: real records are
// incomplete, and the assistant must not read a missing meal as "not eaten".
function mealsFor(date, i, isToday) {
  const out = []
  // No breakfast logged every third day.
  if (i % 3 !== 1) out.push(meal(date, 'breakfast', breakfasts[i % breakfasts.length], i % 2 === 0))
  out.push(meal(date, 'lunch', lunches[i % lunches.length], i % 4 !== 3))
  // Today's dinner has not happened yet.
  if (!isToday) out.push(meal(date, 'dinner', dinners[i % dinners.length], i % 5 !== 4))
  if (i % 4 === 2) out.push(meal(date, 'snack', ['蛋白棒', 200, 20, 7, 18], true))
  return out
}

function event(date, title, start, end, notes) {
  return { title, starts_at: `${date}T${start}:00${ZONE}`, ends_at: `${date}T${end}:00${ZONE}`, notes: notes ?? null }
}

function eventsFor(date) {
  const wd = weekday(date)
  const out = []
  if (wd >= 1 && wd <= 5) out.push(event(date, '专业课', '09:00', '12:10'))
  if (wd === 2 || wd === 4) out.push(event(date, '实验课', '13:00', '16:10', '实验室不能带食物'))
  if (wd === 1 || wd === 3 || wd === 5) out.push(event(date, '健身房：力量训练', '18:00', '19:30'))
  if (wd === 6) out.push(event(date, '便利店打工', '10:00', '16:00'))
  if (wd === 0) out.push(event(date, '和朋友去超市采购', '15:00', '16:30'))
  return out
}

// --- Run ---

const from = tokyoDate(-(DAYS - 1))
const to = tokyoDate(1)
console.log(`Seeding ${username} at ${baseUrl}, ${from} to ${to}`)

await must('GET', '/api/auth/csrf')
const login = await api('POST', '/api/auth/login', { username, password })
if (login.status === 200) {
  console.log('Logged in to the existing account')
} else if (login.status === 401) {
  await must('POST', '/api/auth/register', { username, password }, [201])
  console.log('Registered a new account')
} else {
  throw new Error(`login -> ${login.status} ${JSON.stringify(login.data?.error)}`)
}

await must('PUT', '/api/profile', profile)

// Clear the window first, so a second run does not duplicate anything.
const oldEvents = (await must('GET', `/api/schedule-events?from=${from}&to=${to}`)).items
for (const e of oldEvents) await must('DELETE', `/api/schedule-events/${e.id}`)
const oldMeals = (await must('GET', `/api/meal-records?from=${from}&to=${to}`)).items
for (const m of oldMeals) await must('DELETE', `/api/meal-records/${m.id}`)

let events = 0
let meals = 0
for (let i = 0; i <= DAYS; i++) {
  const date = tokyoDate(i - (DAYS - 1))
  const isToday = i === DAYS - 1
  const isTomorrow = i === DAYS
  for (const e of eventsFor(date)) {
    await must('POST', '/api/schedule-events', e, [201])
    events++
  }
  // Tomorrow has a schedule but no meals yet.
  if (isTomorrow) continue
  for (const m of mealsFor(date, i, isToday)) {
    await must('POST', '/api/meal-records', m, [201])
    meals++
  }
}

await must('POST', '/api/auth/logout', undefined, [204])
console.log(`Done: replaced ${oldEvents.length} events and ${oldMeals.length} meals with ${events} events and ${meals} meals`)
