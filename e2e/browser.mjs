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
  await fill('select[name=campus]', 'yoshida')
  await fill('input[name=password]', 'correct horse battery')
  await click('form button[type=submit]')
  await waitFor(`location.pathname === '/profile'`, 'redirect to /profile after register')
  const sessionCsrf = await getCookie('mogumogu_csrf')
  check('register logs in and goes to /profile (has_profile=false)', true)
  check('CSRF cookie rotated to a session token on register',
    anonCsrf?.startsWith('v1.a.') && sessionCsrf?.startsWith('v1.s.') && anonCsrf !== sessionCsrf,
    `${anonCsrf?.slice(0, 5)} -> ${sessionCsrf?.slice(0, 5)}`)
  check('welcome banner on first profile', (await bodyText()).includes('ようこそ'))

  // The campus is saved on its own, before any profile exists.
  const campusNow = `fetch('/api/auth/me').then(r => r.json()).then(r => r.user.campus)`
  await fill('select[name=campus]', 'uji')
  await waitFor(`${campusNow}.then(c => c === 'uji')`, 'campus saved')
  await waitFor(`document.querySelector('.campus [role=status]')?.innerText === '保存しました。'`, 'campus saved message')
  check('campus changes without a profile, with a saved message', true)
  await fill('select[name=campus]', 'yoshida')
  await waitFor(`${campusNow}.then(c => c === 'yoshida')`, 'campus saved back')
  await waitFor(`!document.querySelector('.campus select').disabled`, 'campus select enabled')
  check('campus changed back', true)

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
  await click('form button[type=submit]')
  await waitFor(`location.pathname === '/'`, 'calendar after profile')
  check('saving the first profile opens the calendar', true)

  // --- calendar ---
  const today = tokyoDate(0)
  const tomorrow = tokyoDate(1)
  const yesterday = tokyoDate(-1)
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
  // Breakfast is a weekday service. Unit/integration tests also fix the day for weekend CI.
  const dayOfWeek = new Date(today + 'T12:00:00Z').getUTCDay()
  if (dayOfWeek > 0 && dayOfWeek < 6) {
    await clickText('.panel .chip', '今日の朝食は何を食べたらいい？')
    await waitFor(`document.querySelectorAll('.panel .rec select option').length === 2`, 'breakfast variants')
    check('one breakfast card for two available plates', await evaluate("document.querySelectorAll('.panel .rec').length") === 1)
    await fill('.panel .rec select', '202')
    await click('.panel .rec .add')
    await waitFor(`document.querySelector('.panel .rec form .nutrients input')?.value === '420'`, 'selected breakfast nutrition')
    await fill('.panel .rec form input[type=date]', tomorrow)
    await click('.panel .rec form button[type=submit]')
    await waitFor(`document.querySelector('.panel .rec button.add')?.disabled === true`, 'selected breakfast saved')
    const breakfast = JSON.parse(requests.filter(r => r.url === '/api/meal-records' && r.method === 'POST').at(-1).body)
    check('saves only the selected breakfast plate', breakfast.content === '朝 洋風' && breakfast.calories === 420 && breakfast.meal_type === 'breakfast')
    await click('.panel .new')
    await waitFor(`!!document.querySelector('.panel .intro .chip')`, 'lunch conversation')
  }
  await clickText('.panel .chip', '今日の昼食は何を食べたらいい？')
  const cardSelector = '.panel .rec'
  await waitFor(`document.querySelectorAll('.panel .rec').length === 2`, 'two menu-backed cards')
  const firstAsk = JSON.parse(requests.filter(r => r.url === '/api/assistant/messages').at(-1).body)
  const firstCard = await textOf(cardSelector)
  check('lunch card shows all eligible stores and official nutrition', firstAsk.locale === 'ja' && firstAsk.date === today &&
    firstCard.includes('中央食堂') && firstCard.includes('北部食堂') && firstCard.includes('450 kcal') && firstCard.includes(today))
  const panelText = await textOf('.panel')
  check('official allergen labels are shown by name, not by site code',
    panelText.includes('卵 · 小麦由来') && !panelText.includes('wheat_origin'))
  const shopLabels = `[...document.querySelector('.panel .rec').querySelectorAll('.shops label')]`
  check('a store without today’s menu is listed after the stores with it', await evaluate(`${shopLabels}.map(l => l.textContent.replace(/\s+/g, ' ').trim()).join('|')`) ===
    '中央食堂 吉田|北部食堂 吉田|吉田食堂 吉田')
  check('today’s menu shows no earlier-menu notice', !firstCard.includes('まだ更新されていません'))
  await evaluate(`${shopLabels}[2].querySelector('input').click()`)
  await waitFor(`document.querySelector('.panel .rec').innerText.includes('まだ更新されていません')`, 'earlier-menu notice')
  const earlierCard = await textOf(cardSelector)
  const short = (d) => `${Number(d.slice(5, 7))}/${Number(d.slice(8))}`
  check('a store’s earlier menu shows its date and that today’s is not updated yet', earlierCard.includes(yesterday) &&
    earlierCard.includes(`${short(yesterday)} のメニューです。${short(today)} のメニューはまだ更新されていません`))
  await evaluate(`${shopLabels}[0].querySelector('input').click()`)
  await click('.panel .close')
  await click('.fab')
  await waitFor(`document.querySelectorAll('.panel .rec').length === 2`, 'cards after reopening')

  const firstRec = "document.querySelector('.panel .rec')"
  await click('.panel .rec .add')
  await waitFor(`!!${firstRec}.querySelector('form')`, 'meal form inside cafeteria card')
  const prefilled = await evaluate(`[...${firstRec}.querySelectorAll('form .nutrients input')].map(i => i.value).join('|')`)
  check('serving uses official values and leaves a missing nutrient blank', prefilled === '450|20|10|', prefilled)
  await fill('.panel .rec form input[type=date]', tomorrow)
  await click('.panel .rec form button[type=submit]')
  await waitFor(`${firstRec}.querySelector('button.add')?.disabled === true`, 'cafeteria meal saved')
  const saved = requests.filter(r => r.url === '/api/meal-records' && r.method === 'POST').map(r => JSON.parse(r.body)).at(-1)
  check('one confirmed meal reaches the calendar', saved.date === tomorrow && saved.meal_type === 'lunch' &&
    saved.calories === 450 && saved.carbs_g === null && saved.content.includes('E2E'))

  const tomorrowCellExpr = `[...document.querySelectorAll('.day')].find(d => d.classList.contains('today')).nextElementSibling`
  await evaluate(`(${tomorrowCellExpr}).click()`)
  await waitFor(`!document.querySelector('.panel .target .today-mark')`, 'panel follows the selected date')
  // Tomorrow is answered at once from today's menu (plans/simplify-hours.md).
  await fill('#assistant-input', '明日の夕食は何を食べたらいい？')
  await click('.composer button[type=submit]')
  await waitFor(`document.querySelectorAll('.panel .rec').length === 4`, 'two dinner cards for tomorrow')
  const ask = JSON.parse(requests.filter(r => r.url === '/api/assistant/messages').at(-1).body)
  check('future request keeps the selected date and both previous card summaries', ask.date === tomorrow &&
    ask.history.length === 1 && ask.history[0].recommendations.length === 2)
  const reference = "document.querySelectorAll('.panel .rec')[2]"
  check('tomorrow’s card uses today’s menu and says tomorrow’s is not updated yet', (await evaluate(`${reference}.innerText`))
    .includes(`${short(today)} のメニューです。${short(tomorrow)} のメニューはまだ更新されていません`))
  await evaluate(`${reference}.querySelector('button.add').click()`)
  await waitFor(`!!${reference}.querySelector('form')`, 'meal form for tomorrow’s card')
  check('tomorrow’s card is recorded on tomorrow', await evaluate(`${reference}.querySelector('form input[type=date]').value`) === tomorrow)
  await evaluate(`[...${reference}.querySelectorAll('form button[type=button]')].at(-1).click()`)
  await evaluate('window.__beforeReload = true')
  await send('Page.reload')
  await waitFor(`!window.__beforeReload && !!document.querySelector('.fab')`, 'calendar after reload')
  await click('.fab')
  await waitFor(`document.querySelectorAll('.panel .rec').length === 4`, 'menu cards after reload')
  check('a saved card stays marked after reload and switching dates', await evaluate(`${firstRec}.querySelector('button.add').disabled`))
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
  check('a new chat stays empty after a reload', await evaluate(`!!${q('.panel .intro')} && !document.querySelector('.panel .bubble')`))

  // Pro: the assistant proposes a record; nothing is saved until it runs. A
  // preference in the same message is saved at once, with a note under the
  // reply. (One message for both: the assistant allows 6 a minute.)
  const ramenOnCalendar = `[...document.querySelectorAll('.meal-group')].some(e => e.innerText.includes('ラーメン'))`
  const memories = `fetch('/api/memories').then(r => r.json()).then(r => r.items.map(m => m.content))`
  await fill('#assistant-input', '昼食にラーメンを食べた、辛いものが苦手って覚えて')
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

  await waitFor(`!!document.querySelector('.panel .memory-note')`, 'memory note')
  await click('.panel .memory-note summary')
  const note = await textOf('.panel .memory-note')
  check('a memory is saved without a card, and the note says so',
    note.includes('記憶を更新しました') && note.includes('覚えた') && note.includes('辛いものが苦手') &&
      (await evaluate(memories)).join() === '辛いものが苦手', note.replace(/\n/g, ' | '))
  await click('.panel .memory-note .undo')
  await waitFor(`document.querySelector('.panel .memory-note')?.innerText.includes('取り消しました')`, 'memory undone')
  check('✕ undoes a new memory', (await evaluate(memories)).length === 0)
  await click('.panel .close')

  // Delete the event (accept the confirm dialog).
  await evaluate(`window.confirm = () => true`)
  await evaluate(`[...document.querySelectorAll('.items button.link.danger')][0].click()`)
  await waitFor(`!document.body.innerText.includes('E2E 夜間の実験')`, 'event deleted')
  check('event deleted after confirmation', (await bodyText()).includes('この日の予定はまだありません'))

  // Profile page shows the saved values.
  await evaluate(`[...document.querySelectorAll('nav a')].find(a => a.innerText==='プロフィール').click()`)
  // The campus select renders before the profile has loaded; wait for the profile form too.
  await waitFor(`location.pathname === '/profile' && !!document.querySelector('select[name=campus]') && [...document.querySelectorAll('label.inline')].some(l => l.innerText.trim()==='アレルゲンがある')`, 'profile opened')
  check('profile shows the registered campus', await evaluate("document.querySelector('[name=campus]').value") === 'yoshida')
  await evaluate(`[...document.querySelectorAll('label.inline')].find(l => l.innerText.trim()==='アレルゲンがある').querySelector('input').click()`)
  await settle()
  await fill('input[placeholder^="例：ピーナッツ"]', 'ピーナッツ、えび')
  await click('form button[type=submit]')
  await waitFor(`!!document.querySelector('.banner.success')`, 'profile allergies saved')
  // Memories can also be added and edited by hand on the Profile page.
  await waitFor(`!!document.querySelector('#memories .add input')`, 'memory form')
  await fill('#memories .add input', '魚が好き')
  await click('#memories .add button')
  await waitFor(`document.querySelector('#memories .list')?.innerText.includes('魚が好き')`, 'memory added')
  await clickText('#memories button', '編集')
  await fill('#memories .list input', '白身魚が好き')
  await clickText('#memories button', '保存')
  await waitFor(`document.querySelector('#memories .list')?.innerText.includes('白身魚が好き') && !document.querySelector('#memories .list input')`, 'memory edited')
  check('memories can be added and edited on the Profile page',
    (await evaluate(memories)).join() === '白身魚が好き' && (await textOf('#memories .count')) === '1 / 30')

  // Back on the calendar, the conversation from before is still there.
  await evaluate(`[...document.querySelectorAll('nav a')].find(a => a.innerText==='カレンダー').click()`)
  await waitFor(`location.pathname === '/' && !!document.querySelector('.fab')`, 'calendar again')
  await click('.fab')
  await waitFor(`!!document.querySelector('.panel .memory-note')`, 'conversation after visiting the profile')
  check('leaving the calendar keeps the conversation', (await textOf('.panel .bubble.user')).includes('ラーメン'))
  await click('.panel .close')

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
  await waitFor(`document.querySelector('.panel .bubble.assistant')?.innerText.includes('verify your restrictions')`, 'English allergy reply')
  const askEn = requests.filter((r) => r.url === '/api/assistant/messages').at(-1)
  check('an English question asks for, and gets, an English reply',
    JSON.parse(askEn.body).locale === 'en' && (await textOf('.panel .bubble.assistant')).includes('verify your restrictions'), askEn.body)
  check('logging out ends the conversation: the next message has no history', JSON.parse(askEn.body).history.length === 0, askEn.body)
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
