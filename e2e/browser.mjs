// Browser end-to-end test: drives headless Chrome over the DevTools Protocol
// through the whole UI (register, profile, calendar, logout, login), in
// Japanese, then switches to English.
//
// Normally started by e2e/run.sh (`make test-e2e`), which provides a throwaway
// database and servers. Every account it creates gets a random name, so runs
// never collide.
//
// usage: node e2e/browser.mjs <empty-profile-dir>
// env:   E2E_BASE_URL  frontend URL (default http://localhost:5173)
//        CHROME        Chrome/Chromium executable (auto-detected otherwise)
// Needs Node 24+ (global WebSocket and fetch); no npm packages.

import { execFileSync, spawn } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { setTimeout as sleep } from 'node:timers/promises'

const profileDir = process.argv[2]
if (!profileDir) {
  console.error('usage: node e2e/browser.mjs <empty-profile-dir>')
  process.exit(2)
}
const BASE = process.env.E2E_BASE_URL ?? 'http://localhost:5173'

// Changes an account tier the only way there is: cmd/admin on the database.
function setTier(username, tier) {
  if (!process.env.E2E_DB) throw new Error('E2E_DB is not set (run through e2e/run.sh)')
  execFileSync('go', ['run', './cmd/admin', 'tier', username, tier], {
    cwd: new URL('../backend', import.meta.url).pathname,
    env: { ...process.env, DB_NAME: process.env.E2E_DB },
    stdio: 'ignore',
  })
}

function findChrome() {
  if (process.env.CHROME) {
    if (existsSync(process.env.CHROME)) return process.env.CHROME
    console.error(`CHROME=${process.env.CHROME} does not exist`)
    process.exit(2)
  }
  const mac = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
  if (existsSync(mac)) return mac
  for (const name of ['google-chrome', 'google-chrome-stable', 'chromium', 'chromium-browser']) {
    try {
      return execFileSync('which', [name], { encoding: 'utf8' }).trim()
    } catch {}
  }
  console.error('Chrome or Chromium not found; set CHROME=/path/to/chrome')
  process.exit(2)
}

// Port 0 lets Chrome pick a free debugging port and write it to DevToolsActivePort.
const chrome = spawn(findChrome(), [
  '--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check',
  '--remote-debugging-port=0', `--user-data-dir=${profileDir}`, 'about:blank',
], { stdio: 'ignore' })

let ws, seq = 0
const pending = new Map()
const requests = [] // {method, url, csrf, status}
const results = []
const sentOrigins = new Map() // extra-info may arrive before requestWillBeSent

function check(name, ok, detail = '') {
  results.push({ name, ok, detail })
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? '  — ' + detail : ''}`)
}

function send(method, params = {}) {
  const id = ++seq
  ws.send(JSON.stringify({ id, method, params }))
  return new Promise((resolve, reject) => pending.set(id, { resolve, reject }))
}

async function connect() {
  for (let i = 0; i < 100; i++) {
    try {
      const port = readFileSync(join(profileDir, 'DevToolsActivePort'), 'utf8').split('\n')[0]
      const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
      const page = targets.find((t) => t.type === 'page')
      if (page) {
        ws = new WebSocket(page.webSocketDebuggerUrl)
        await new Promise((r) => ws.addEventListener('open', r, { once: true }))
        ws.addEventListener('message', (ev) => {
          const msg = JSON.parse(ev.data)
          if (msg.id && pending.has(msg.id)) {
            const p = pending.get(msg.id)
            pending.delete(msg.id)
            msg.error ? p.reject(new Error(msg.error.message)) : p.resolve(msg.result)
          } else if (msg.method === 'Network.requestWillBeSent') {
            const r = msg.params.request
            if (r.url.includes('/api/')) {
              requests.push({ id: msg.params.requestId, method: r.method, url: r.url.replace(BASE, ''), body: r.postData, csrf: r.headers['X-CSRF-Token'], origin: sentOrigins.get(msg.params.requestId) })
            }
          } else if (msg.method === 'Network.requestWillBeSentExtraInfo') {
            // The headers actually sent, including Origin which the browser adds late.
            const h = msg.params.headers
            const origin = h.Origin ?? h.origin
            const r = requests.find((x) => x.id === msg.params.requestId)
            if (r) r.origin = origin
            else sentOrigins.set(msg.params.requestId, origin)
          } else if (msg.method === 'Network.responseReceived') {
            const r = requests.find((x) => x.id === msg.params.requestId)
            if (r) r.status = msg.params.response.status
          }
        })
        return
      }
    } catch {}
    await sleep(200)
  }
  throw new Error('could not connect to Chrome')
}

async function evaluate(expr) {
  const r = await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true })
  if (r.exceptionDetails) throw new Error(`${expr}\n${r.exceptionDetails.exception?.description ?? r.exceptionDetails.text}`)
  return r.result.value
}

async function waitFor(expr, label, timeout = 8000) {
  const end = Date.now() + timeout
  while (Date.now() < end) {
    try { if (await evaluate(expr)) return true } catch {}
    await sleep(100)
  }
  throw new Error(`timed out waiting for: ${label ?? expr}`)
}

const q = (sel) => `document.querySelector(${JSON.stringify(sel)})`
const textOf = (sel) => evaluate(`${q(sel)}?.innerText ?? ''`)
const bodyText = () => evaluate('document.body.innerText')
const path = () => evaluate('location.pathname')
async function click(sel) { await evaluate(`${q(sel)}.click()`) }
async function clickText(tag, text) {
  await evaluate(`[...document.querySelectorAll(${JSON.stringify(tag)})].find(e => e.innerText.trim() === ${JSON.stringify(text)}).click()`)
}
async function fill(sel, value) {
  await evaluate(`(() => { const e = ${q(sel)}; e.value = ${JSON.stringify(value)}; e.dispatchEvent(new Event('input', {bubbles:true})); e.dispatchEvent(new Event('change', {bubbles:true})) })()`)
}
async function getCookie(name) {
  const { cookies } = await send('Network.getAllCookies')
  return cookies.find((c) => c.name === name)?.value
}
const settle = () => sleep(400)

function tokyoDate(offsetDays = 0) {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Tokyo' }).format(new Date(Date.now() + offsetDays * 864e5))
}

async function main() {
  await connect()
  await send('Network.enable')
  await send('Page.enable')
  // A Japanese browser: the main flow runs in Japanese, picked automatically.
  await send('Network.setUserAgentOverride', { userAgent: await evaluate('navigator.userAgent'), acceptLanguage: 'ja-JP,ja' })
  await send('Page.navigate', { url: BASE + '/' })
  await waitFor(`location.pathname === '/login'`, 'redirect to /login')
  check('unauthenticated visit to / redirects to /login', true)
  check('a Japanese browser gets the Japanese interface', (await evaluate('document.documentElement.lang')) === 'ja' &&
    (await bodyText()).includes('ログイン'))

  // --- register ---
  const username = 'e2e_ui_' + Math.random().toString(36).slice(2, 8)
  await clickText('button', '新規登録')
  await fill('input[name=username]', username)
  await fill('input[name=password]', 'short')
  await click('form button[type=submit]')
  await waitFor(`document.body.innerText.includes('8文字以上')`, 'password field error')
  check('register validation error shown under the field', true)

  await waitFor(`!!document.cookie.match(/mogumogu_csrf=v1\\.a\\./)`, 'anonymous csrf cookie')
  const anonCsrf = await getCookie('mogumogu_csrf')
  await fill('input[name=password]', 'correct horse battery')
  await click('form button[type=submit]')
  await waitFor(`location.pathname === '/profile'`, 'redirect to /profile after register')
  const sessionCsrf = await getCookie('mogumogu_csrf')
  check('register logs in and goes to /profile (has_profile=false)', true)
  check('CSRF cookie rotated to a session token on register',
    anonCsrf?.startsWith('v1.a.') && sessionCsrf?.startsWith('v1.s.') && anonCsrf !== sessionCsrf,
    `${anonCsrf?.slice(0, 5)} -> ${sessionCsrf?.slice(0, 5)}`)
  check('welcome banner on first profile', (await bodyText()).includes('ようこそ'))

  // Navigating to the calendar before having a profile bounces back.
  await evaluate(`history.pushState({}, '', '/'); dispatchEvent(new PopStateEvent('popstate'))`)
  await settle()
  check('calendar requires a profile first', (await path()) === '/profile')

  // --- profile ---
  await click('form button[type=submit]')
  await waitFor(`document.querySelectorAll('.field-error').length >= 4`, 'required errors')
  check('empty profile shows required errors', true, `${await evaluate(`document.querySelectorAll('.field-error').length`)} field errors`)
  await fill('input[type=number][min="50"]', '301')
  await click('form button[type=submit]')
  await waitFor(`document.body.innerText.includes('50〜300の値を入力してください')`, 'height range error')
  check('out-of-range height shows range message', true)

  await fill('input[type=number][min="50"]', '172.5')
  await fill('input[type=number][min="10"][max="500"]', '65')
  await fill('input[type=number][max="120"]', '21')
  await evaluate(`[...document.querySelectorAll('label.inline')].find(l => l.innerText.trim()==='ある').querySelector('input').click()`)
  await evaluate(`[...document.querySelectorAll('label.inline')].find(l => l.innerText.trim()==='筋肉をつける').querySelector('input').click()`)
  await evaluate(`[...document.querySelectorAll('label.inline')].find(l => l.innerText.trim()==='アレルゲンがある').querySelector('input').click()`)
  await settle()
  await fill('input[placeholder^="例：ピーナッツ"]', 'ピーナッツ、えび')
  await click('form button[type=submit]')
  await waitFor(`location.pathname === '/'`, 'calendar after profile')
  check('saving the first profile opens the calendar', true)

  // --- calendar ---
  const today = tokyoDate(0)
  const tomorrow = tokyoDate(1)
  const [, m] = today.split('-').map(Number)
  await waitFor(`!!document.querySelector('.day.today.selected')`, 'today selected')
  check('calendar shows the current month with today selected', (await textOf('.month-nav h1')).includes(`${m}月`))
  const headers = await evaluate(`[...document.querySelectorAll('.weekday')].map(e => e.innerText)`)
  check('week starts on Monday', headers[0] === '月' && headers[6] === '日', headers.join(' '))

  // --- meal assistant (the AI service runs with the fake model) ---
  check('old Ask AI button is gone', !(await evaluate(`!!${q('.ask-ai')}`)))
  await click('.fab')
  await waitFor(`!!document.querySelector('.panel .intro .chip')`, 'assistant panel with presets')
  const presets = await evaluate(`[...document.querySelectorAll('.panel .intro .chip')].map(e => e.innerText)`)
  check('assistant opens with 4 preset questions for today', presets.length === 4 && presets[1] === '今日の昼食は何を食べたらいい？', presets.join(' / '))
  await clickText('.panel .chip', '今日の昼食は何を食べたらいい？')
  await waitFor(`document.querySelectorAll('.panel .rec').length === 3`, 'three recommendation cards')
  const firstAsk = requests.find((r) => r.url === '/api/assistant/messages')
  check('preset question gets 3 recommendation cards; presets hidden',
    !(await evaluate(`!!${q('.panel .intro')}`)) && JSON.parse(firstAsk.body).date === today, firstAsk.body)
  check('the question carries the interface language, and the reply is in it',
    JSON.parse(firstAsk.body).locale === 'ja' && (await textOf('.panel .bubble.assistant')).startsWith('（テスト回答）'))

  await click('.panel .close')
  await waitFor(`!document.querySelector('.panel')`, 'panel closed')
  await click('.fab')
  await waitFor(`!!document.querySelector('.panel')`, 'panel reopened')
  check('closing and reopening keeps the conversation', await evaluate(`document.querySelectorAll('.panel .rec').length === 3`))

  // Another date: the conversation continues below a divider.
  const tomorrowCellExpr = `[...document.querySelectorAll('.day')].find(d => d.classList.contains('today')).nextElementSibling`
  await evaluate(`(${tomorrowCellExpr}).click()`)
  await waitFor(`!document.querySelector('.panel .target .today-mark')`, 'panel follows the selected date')
  await fill('#assistant-input', 'その日は何を食べたらいい？')
  await click('.composer button[type=submit]')
  await waitFor(`document.querySelectorAll('.panel .follow-up .chip').length === 3`, 'meal choice buttons')
  const divider = await textOf('.panel .divider')
  check('new date: divider shown, earlier messages kept', divider.startsWith('ここから') && (await evaluate(`document.querySelectorAll('.panel .rec').length`)) === 3, divider)
  await clickText('.panel .follow-up .chip', '明日の夕食は何を食べたらいい？')
  await waitFor(`document.querySelectorAll('.panel .rec').length === 6`, 'recommendations for tomorrow')
  const asks = requests.filter((r) => r.url === '/api/assistant/messages').map((r) => JSON.parse(r.body))
  check('each message carries its own target date',
    asks.length === 3 && asks[0].date === today && asks[1].date === tomorrow && asks[2].date === tomorrow && asks[2].message === '明日の夕食は何を食べたらいい？',
    asks.map((a) => `${a.date} ${a.message}`).join(' | '))
  check('each message carries the answered turns before it as history',
    asks[0].history.length === 0 && asks[1].history.length === 1 && asks[1].history[0].recommendations.length === 3 &&
    asks[2].history.length === 2 && asks[2].history[1].message === 'その日は何を食べたらいい？' && !('reason' in asks[1].history[0].recommendations[0]),
    asks.map((a) => a.history.length).join(' / '))

  // A follow-up only makes sense with the history: the fake model names the
  // earlier recommendation it builds on.
  await fill('#assistant-input', 'ご飯なしで')
  await click('.composer button[type=submit]')
  await waitFor(`document.querySelectorAll('.panel .rec').length === 9`, 'follow-up recommendations')
  const followUp = await evaluate(`[...document.querySelectorAll('.panel .bubble.assistant')].at(-1).innerText`)
  const followAsk = requests.filter((r) => r.url === '/api/assistant/messages').map((r) => JSON.parse(r.body)).at(-1)
  check('a follow-up is answered from the history', followUp.includes('照り焼きチキン丼') && followAsk.history.length === 3, followUp)

  // A recommendation shows its estimate and can be added to the meal log.
  const tomorrowRec = `document.querySelectorAll('.panel .rec')[3]`
  const estimate = await evaluate(`[...${tomorrowRec}.querySelectorAll('.kcal, .macro, .per100')].map(e => e.textContent.trim()).join(' | ')`)
  check('recommendation card shows the estimate per serving and per 100 g',
    estimate === '約 650 kcal（目安） | P 35g | F 18g | C 85g | 約 163 kcal / 100g · P 9g F 4g C 21g / 100g（目安）', estimate)
  await evaluate(`${tomorrowRec}.querySelector('button.add').click()`)
  await waitFor(`!!${tomorrowRec}.querySelector('form select')`, 'meal form inside the card')
  const prefilled = await evaluate(`(() => { const f = ${tomorrowRec}.querySelector('form'); return [f.querySelector('input[type=date]').value, f.querySelector('select').value, f.querySelector('input[maxlength="200"]').value, ...[...f.querySelectorAll('input[type=number]')].map(i => i.value)].join('|') })()`)
  check('the meal form is prefilled from the recommendation', prefilled === `${tomorrow}|dinner|照り焼きチキン丼|650|35|18|85`, prefilled)
  await evaluate(`${tomorrowRec}.querySelector('form button[type=submit]').click()`)
  await waitFor(`${tomorrowRec}.querySelector('button.add')?.disabled === true`, 'recommendation added')
  const saved = requests.filter((r) => r.url === '/api/meal-records' && r.method === 'POST').map((r) => JSON.parse(r.body)).at(-1)
  check('adding saves the serving estimate on the target date; the button says so',
    saved?.date === tomorrow && saved.meal_type === 'dinner' && saved.calories === 650 &&
    (await evaluate(`${tomorrowRec}.querySelector('button.add').innerText`)).includes('追加しました'), JSON.stringify(saved))
  await waitFor(`(${tomorrowCellExpr}).getAttribute('aria-label').includes('夕食')`, 'calendar reloaded with the new dinner')
  check('the calendar shows the added meal right away', true)

  await click('.panel .new')
  await waitFor(`!!document.querySelector('.panel .intro .chip') && !document.querySelector('.panel .bubble')`, 'new chat')
  check('a new chat clears the conversation', !(await evaluate(`!!${q('.panel .new')}`)))
  await click('.panel .close')
  await clickText('button', '今日')
  await waitFor(`!!document.querySelector('.day.today.selected')`, 'back to today')

  // Overnight event on today -> appears on today and tomorrow.
  await clickText('button', '+ 予定を追加')
  await waitFor(`!!document.querySelector('input[type=datetime-local]')`, 'event form')
  await fill('.form input[maxlength="100"]', 'E2E 夜間の実験')
  const dt = await evaluate(`[...document.querySelectorAll('input[type=datetime-local]')].map(e=>e.value)`)
  check('new event defaults to the selected date', dt[0] === `${today}T09:00`, dt[0])
  await evaluate(`(() => { const [s,e] = document.querySelectorAll('input[type=datetime-local]'); s.value='${today}T10:00'; s.dispatchEvent(new Event('input',{bubbles:true})); e.value='${today}T09:00'; e.dispatchEvent(new Event('input',{bubbles:true})) })()`)
  await clickText('button', '保存')
  await waitFor(`document.body.innerText.includes('終了は開始より後にしてください')`, 'end before start error')
  check('end-before-start error shown on the field', true)
  await evaluate(`(() => { const [s,e] = document.querySelectorAll('input[type=datetime-local]'); s.value='${today}T23:00'; s.dispatchEvent(new Event('input',{bubbles:true})); e.value='${tomorrow}T01:00'; e.dispatchEvent(new Event('input',{bubbles:true})) })()`)
  await clickText('button', '保存')
  await waitFor(`document.body.innerText.includes('E2E 夜間の実験') && !document.querySelector('input[type=datetime-local]')`, 'event saved')
  const badgeToday = await evaluate(`document.querySelector('.day.today .badge.event')?.innerText`)
  const tomorrowCell = `[...document.querySelectorAll('.day')].find(d => d.getAttribute('aria-label') && d.classList.contains('today')).nextElementSibling`
  const badgeTomorrow = await evaluate(`(${tomorrowCell})?.querySelector('.badge.event')?.innerText`)
  check('overnight event shown on today and tomorrow', badgeToday === '予定 1' && badgeTomorrow === '予定 1', `${badgeToday} / ${badgeTomorrow}`)
  check('overnight event time shows the next date', (await textOf('.items .time')).includes('23:00 – '))

  // Meal with unknown calories and zero protein.
  await clickText('button', '+ 記録を追加')
  await waitFor(`!!document.querySelector('select')`, 'meal form')
  await fill('select', 'lunch')
  await fill('.form input[maxlength="200"]', 'E2E サラダチキン')
  await evaluate(`(() => { const ins = document.querySelectorAll('.nutrients input'); ins[1].value='0'; ins[1].dispatchEvent(new Event('input',{bubbles:true})) })()`)
  await clickText('button', '保存')
  await waitFor(`document.body.innerText.includes('E2E サラダチキン') && !document.querySelector('select')`, 'meal saved')
  const nutrients = await textOf('.meal-group .nutrients')
  check('blank calories shown as not entered, zero protein as 0', nutrients.includes('エネルギー 未入力') && nutrients.includes('たんぱく質 0 g'), nutrients.replace(/\n/g, ' | '))

  // Edit the meal.
  await evaluate(`[...document.querySelectorAll('.meal-group button.link')].find(b => b.innerText==='編集').click()`)
  await waitFor(`!!document.querySelector('select')`, 'meal edit form')
  check('edit form keeps blank nutrition blank', (await evaluate(`document.querySelectorAll('.nutrients input')[0].value`)) === '')
  await fill('.form input[maxlength="200"]', 'E2E サラダチキン（大盛り）')
  await clickText('button', '保存')
  await waitFor(`document.body.innerText.includes('E2E サラダチキン（大盛り）') && !document.querySelector('select')`, 'meal edited')
  check('meal edited', true)

  check('a new account shows the Normal tier', (await textOf('.account .tier')) === 'Normal', await textOf('.account .tier'))
  // Made Pro before the reload, which then shows the badge.
  setTier(username, 'pro')

  // Persistence: reload. Page.reload returns before the new document exists, so
  // mark the old one and wait until the mark is gone and the data is rendered.
  await evaluate('window.__beforeReload = true')
  await send('Page.reload')
  await waitFor(`!window.__beforeReload && document.querySelectorAll('.items button.link.danger').length >= 2 &&
    document.body.innerText.includes('E2E 夜間の実験') && document.body.innerText.includes('（大盛り）')`, 'data after reload')
  check('data still there after reload', true)
  check('after the admin command makes the account Pro, the tier shows Pro', (await textOf('.account .tier')) === 'Pro', await textOf('.account .tier'))
  await click('.fab')
  await waitFor(`!!document.querySelector('.panel')`, 'panel after reload')
  check('reload starts a new conversation', await evaluate(`!!${q('.panel .intro')} && !document.querySelector('.panel .bubble')`))

  // Pro: the assistant proposes a record; nothing is saved until it runs.
  const ramenOnCalendar = `[...document.querySelectorAll('.meal-group')].some(e => e.innerText.includes('ラーメン'))`
  await fill('#assistant-input', '昼食にラーメンを食べた')
  await click('.composer button[type=submit]')
  await waitFor(`!!document.querySelector('.panel .proposal')`, 'proposal card')
  const card = await textOf('.panel .proposal')
  check('a Pro user gets a card of changes to run',
    card.includes('追加') && card.includes('ラーメン') && card.includes('約 500 kcal（目安）') && card.includes('実行'), card.replace(/\n/g, ' | '))
  check('nothing is saved before the card runs', !(await evaluate(ramenOnCalendar)))
  await click('.panel .proposal .actions button')
  await waitFor(`!!document.querySelector('.panel .proposal .change.done')`, 'proposal run')
  check('after running, each change shows its result and the card locks',
    (await textOf('.panel .proposal .result')) === '✓ 完了' && !(await evaluate(`!!${q('.panel .proposal .actions')}`)))
  await waitFor(ramenOnCalendar, 'calendar shows the recorded meal')
  const run = requests.find((r) => r.url.endsWith('/execute'))
  check('running sends the checked changes; the calendar shows the new record', JSON.parse(run.body).accepted.join() === '0', run.body)
  await click('.panel .close')

  // Delete the event (accept the confirm dialog).
  await evaluate(`window.confirm = () => true`)
  await evaluate(`[...document.querySelectorAll('.items button.link.danger')][0].click()`)
  await waitFor(`!document.body.innerText.includes('E2E 夜間の実験')`, 'event deleted')
  check('event deleted after confirmation', (await bodyText()).includes('この日の予定はまだありません'))

  // Profile page shows the saved values.
  await evaluate(`[...document.querySelectorAll('nav a')].find(a => a.innerText==='プロフィール').click()`)
  await waitFor(`location.pathname === '/profile' && !!document.querySelector('input[placeholder^="例：ピーナッツ"]')`, 'profile with allergens')
  check('profile keeps allergens', (await evaluate(`${q('input[placeholder^="例：ピーナッツ"]')}.value`)) === 'ピーナッツ、えび')

  // CSRF: every write after registration used the session token, none failed.
  const writes = requests.filter((r) => r.method !== 'GET')
  const afterLogin = writes.filter((r) => !r.url.startsWith('/api/auth/register'))
  check('all writes after login used the rotated session token, no retries',
    afterLogin.length > 0 && afterLogin.every((r) => r.csrf === sessionCsrf) && !requests.some((r) => r.status === 403),
    `${afterLogin.length} writes, statuses ${[...new Set(writes.map((r) => r.status))].join(',')}`)
  check('writes carry the browser Origin', writes.length > 0 && writes.every((r) => r.origin === BASE), [...new Set(writes.map((r) => r.origin))].join(','))

  // Logout: old session cookie stops working.
  const oldSession = await getCookie('mogumogu_session')
  await clickText('button', 'ログアウト')
  await waitFor(`location.pathname === '/login'`, 'login after logout')
  const csrfAfterLogout = await getCookie('mogumogu_csrf')
  const res = await fetch(BASE + '/api/auth/me', { headers: { Cookie: `mogumogu_session=${oldSession}` } })
  check('logout: back to login, new anonymous CSRF token, old session cookie rejected',
    csrfAfterLogout?.startsWith('v1.a.') && res.status === 401, `me with old cookie -> ${res.status}`)

  // Login again lands on the calendar.
  await fill('input[name=username]', username.toUpperCase())
  await fill('input[name=password]', 'correct horse battery')
  await click('form button[type=submit]')
  await waitFor(`location.pathname === '/'`, 'calendar after login')
  check('login (case-insensitive username) goes straight to the calendar', true)

  // --- switch to English; the choice outlives a reload ---
  await click('.locale-switch button[lang=en]')
  await settle()
  const navEn = await evaluate(`[...document.querySelectorAll('nav a')].map(a => a.innerText)`)
  const headersEn = await evaluate(`[...document.querySelectorAll('.weekday')].map(e => e.innerText)`)
  check('switching to English changes the interface at once',
    (await evaluate('document.documentElement.lang')) === 'en' && navEn[0] === 'Calendar' && headersEn[0] === 'Mon',
    `${navEn.join(' ')} / ${headersEn.join(' ')}`)
  await click('.fab')
  await waitFor(`!!document.querySelector('.panel .intro .chip')`, 'English presets')
  const presetsEn = await evaluate(`[...document.querySelectorAll('.panel .intro .chip')].map(e => e.innerText)`)
  check('assistant presets follow the language', presetsEn[1] === 'What should I eat for lunch today?', presetsEn.join(' / '))
  await clickText('.panel .chip', 'What should I eat for lunch today?')
  await waitFor(`document.querySelectorAll('.panel .rec').length === 3`, 'English recommendations')
  const askEn = requests.filter((r) => r.url === '/api/assistant/messages').at(-1)
  check('an English question asks for, and gets, an English reply',
    JSON.parse(askEn.body).locale === 'en' && (await textOf('.panel .bubble.assistant')).startsWith('(Test reply)'), askEn.body)
  check('the first message of a new chat has no history', JSON.parse(askEn.body).history.length === 0, askEn.body)
  await evaluate('window.__beforeReload = true')
  await send('Page.reload')
  await waitFor(`!window.__beforeReload && !!document.querySelector('.day.today')`, 'calendar after reload')
  check('the language choice is kept after a reload, over the browser language',
    (await evaluate('document.documentElement.lang')) === 'en' && (await bodyText()).includes('Calendar'))
}

try {
  await main()
} catch (err) {
  check('script', false, err.message)
} finally {
  ws?.close()
  chrome.kill()
  const failed = results.filter((r) => !r.ok).length
  console.log(`\n${results.length - failed}/${results.length} checks passed`)
  process.exit(failed ? 1 : 0)
}
