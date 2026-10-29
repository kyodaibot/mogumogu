// Shared by the seed scripts: a small API client, dates in the product time
// zone, the meal log every seeded account gets, and the reset-and-fill run.
// Everything goes through the public API, the same way the browser does.

const ZONE = '+09:00'

// --- A tiny client: cookies, CSRF header and Origin, like the frontend ---

function createClient(baseUrl) {
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

  return { api, must }
}

// --- Dates in the product time zone ---

export function tokyoDate(offsetDays) {
  const d = new Date(Date.now() + offsetDays * 86_400_000)
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Tokyo' }).format(d) // YYYY-MM-DD
}

export function weekday(date) {
  return new Date(`${date}T12:00:00${ZONE}`).getUTCDay() // 0 = Sunday
}

function nextDay(date) {
  return new Date(Date.parse(`${date}T12:00:00${ZONE}`) + 86_400_000).toISOString().slice(0, 10)
}

/** An event on `date`. An end time not after the start runs into the next day. */
export function event(date, title, start, end, notes) {
  const endDate = end <= start ? nextDay(date) : date
  return {
    title,
    starts_at: `${date}T${start}:00${ZONE}`,
    ends_at: `${endDate}T${end}:00${ZONE}`,
    notes: notes ?? null,
  }
}

// --- The meal log: the same for every seeded account ---

const breakfasts = [
  ['オートミールと牛乳、バナナ', 420, 15, 9, 70],
  ['全粒粉トーストとゆで卵2個', 380, 20, 14, 40],
  ['おにぎりと味噌汁', 350, 9, 4, 68],
  ['ギリシャヨーグルトとグラノーラ', 400, 22, 10, 52],
]
const lunches = [
  ['学食の親子丼', 750, 32, 20, 105],
  ['コンビニのサラダチキンとおにぎり', 520, 34, 9, 72],
  ['豚骨ラーメン', 820, 25, 32, 100],
  ['焼き鮭定食', 680, 35, 18, 85],
  ['カレーライス', 780, 18, 24, 120],
]
const dinners = [
  ['自炊のトマトと卵の炒め物、ご飯', 650, 22, 20, 90],
  ['肉うどん', 600, 26, 14, 88],
  ['焼き魚と野菜', 550, 38, 18, 40],
  ['麻婆豆腐丼', 720, 28, 26, 92],
  ['居酒屋で飲み会', null, null, null, null],
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
  if (i % 4 === 2) out.push(meal(date, 'snack', ['プロテインバー', 200, 20, 7, 18], true))
  return out
}

// --- Run ---

/**
 * Logs in to the account (registering it if it does not exist), replaces its
 * profile, deletes every event and meal from `pastDays - 1` days ago to
 * `futureDays` days ahead, and writes them again: events from `eventsFor` on
 * every day, meals up to today. Records outside the window are left alone.
 */
export async function seedAccount({ baseUrl, username, password, profile, eventsFor, pastDays, futureDays }) {
  const { api, must } = createClient(baseUrl)
  const from = tokyoDate(-(pastDays - 1))
  const to = tokyoDate(futureDays)

  await must('GET', '/api/auth/csrf')
  const login = await api('POST', '/api/auth/login', { username, password })
  let registered = false
  if (login.status === 401) {
    await must('POST', '/api/auth/register', { username, password }, [201])
    registered = true
  } else if (login.status !== 200) {
    throw new Error(`login ${username} -> ${login.status} ${JSON.stringify(login.data?.error)}`)
  }

  await must('PUT', '/api/profile', profile)

  // Clear the window first, so a second run does not duplicate anything.
  const oldEvents = (await must('GET', `/api/schedule-events?from=${from}&to=${to}`)).items
  for (const e of oldEvents) await must('DELETE', `/api/schedule-events/${e.id}`)
  const oldMeals = (await must('GET', `/api/meal-records?from=${from}&to=${to}`)).items
  for (const m of oldMeals) await must('DELETE', `/api/meal-records/${m.id}`)

  let events = 0
  let meals = 0
  for (let i = 0; i < pastDays + futureDays; i++) {
    const date = tokyoDate(i - (pastDays - 1))
    for (const e of eventsFor(date)) {
      await must('POST', '/api/schedule-events', e, [201])
      events++
    }
    // Future days have a schedule but no meals yet.
    if (i >= pastDays) continue
    for (const m of mealsFor(date, i, i === pastDays - 1)) {
      await must('POST', '/api/meal-records', m, [201])
      meals++
    }
  }

  await must('POST', '/api/auth/logout', undefined, [204])
  return { from, to, registered, replaced: { events: oldEvents.length, meals: oldMeals.length }, events, meals }
}
