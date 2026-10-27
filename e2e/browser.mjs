// Browser end-to-end test: drives headless Chrome over the DevTools Protocol
// through the whole UI (register, profile, calendar, logout, login).
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
  await send('Page.navigate', { url: BASE + '/' })
  await waitFor(`location.pathname === '/login'`, 'redirect to /login')
  check('unauthenticated visit to / redirects to /login', true)

  // --- register ---
  const username = 'e2e_ui_' + Math.random().toString(36).slice(2, 8)
  await clickText('button', '注册')
  await fill('input[name=username]', username)
  await fill('input[name=password]', 'short')
  await click('form button[type=submit]')
  await waitFor(`document.body.innerText.includes('至少 8 个字符')`, 'password field error')
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
  check('welcome banner on first profile', (await bodyText()).includes('欢迎'))

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
  await waitFor(`document.body.innerText.includes('请输入 50–300 之间的数值')`, 'height range error')
  check('out-of-range height shows range message', true)

  await fill('input[type=number][min="50"]', '172.5')
  await fill('input[type=number][min="10"][max="500"]', '65')
  await fill('input[type=number][max="120"]', '21')
  await evaluate(`[...document.querySelectorAll('label.inline')].find(l => l.innerText.trim()==='有').querySelector('input').click()`)
  await evaluate(`[...document.querySelectorAll('label.inline')].find(l => l.innerText.trim()==='增肌').querySelector('input').click()`)
  await evaluate(`[...document.querySelectorAll('label.inline')].find(l => l.innerText.trim()==='有过敏原').querySelector('input').click()`)
  await settle()
  await fill('input[placeholder^="例如：花生"]', '花生、虾')
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
  check('week starts on Monday', headers[0] === '周一' && headers[6] === '周日', headers.join(' '))

  // --- meal assistant (the AI service runs with the fake model) ---
  check('old Ask AI button is gone', !(await evaluate(`!!${q('.ask-ai')}`)))
  await click('.fab')
  await waitFor(`!!document.querySelector('.panel .intro .chip')`, 'assistant panel with presets')
  const presets = await evaluate(`[...document.querySelectorAll('.panel .intro .chip')].map(e => e.innerText)`)
  check('assistant opens with 4 preset questions for today', presets.length === 4 && presets[1] === '今天午餐吃什么？', presets.join(' / '))
  await clickText('.panel .chip', '今天午餐吃什么？')
  await waitFor(`document.querySelectorAll('.panel .rec').length === 3`, 'three recommendation cards')
  const firstAsk = requests.find((r) => r.url === '/api/assistant/messages')
  check('preset question gets 3 recommendation cards; presets hidden',
    !(await evaluate(`!!${q('.panel .intro')}`)) && JSON.parse(firstAsk.body).date === today, firstAsk.body)

  await click('.panel .close')
  await waitFor(`!document.querySelector('.panel')`, 'panel closed')
  await click('.fab')
  await waitFor(`!!document.querySelector('.panel')`, 'panel reopened')
  check('closing and reopening keeps the conversation', await evaluate(`document.querySelectorAll('.panel .rec').length === 3`))

  // Another date: the conversation continues below a divider.
  const tomorrowCellExpr = `[...document.querySelectorAll('.day')].find(d => d.classList.contains('today')).nextElementSibling`
  await evaluate(`(${tomorrowCellExpr}).click()`)
  await waitFor(`!document.querySelector('.panel .target .today-mark')`, 'panel follows the selected date')
  await fill('#assistant-input', '那天吃什么？')
  await click('.composer button[type=submit]')
  await waitFor(`document.querySelectorAll('.panel .follow-up .chip').length === 3`, 'meal choice buttons')
  const divider = await textOf('.panel .divider')
  check('new date: divider shown, earlier messages kept', divider.startsWith('以下关于') && (await evaluate(`document.querySelectorAll('.panel .rec').length`)) === 3, divider)
  await clickText('.panel .follow-up .chip', '明天晚餐吃什么？')
  await waitFor(`document.querySelectorAll('.panel .rec').length === 6`, 'recommendations for tomorrow')
  const asks = requests.filter((r) => r.url === '/api/assistant/messages').map((r) => JSON.parse(r.body))
  check('each message carries its own target date',
    asks.length === 3 && asks[0].date === today && asks[1].date === tomorrow && asks[2].date === tomorrow && asks[2].message === '明天晚餐吃什么？',
    asks.map((a) => `${a.date} ${a.message}`).join(' | '))
  await click('.panel .close')
  await clickText('button', '今天')
  await waitFor(`!!document.querySelector('.day.today.selected')`, 'back to today')

  // Overnight event on today -> appears on today and tomorrow.
  await clickText('button', '+ 新增行程')
  await waitFor(`!!document.querySelector('input[type=datetime-local]')`, 'event form')
  await fill('.form input[maxlength="100"]', 'E2E 夜间实验')
  const dt = await evaluate(`[...document.querySelectorAll('input[type=datetime-local]')].map(e=>e.value)`)
  check('new event defaults to the selected date', dt[0] === `${today}T09:00`, dt[0])
  await evaluate(`(() => { const [s,e] = document.querySelectorAll('input[type=datetime-local]'); s.value='${today}T10:00'; s.dispatchEvent(new Event('input',{bubbles:true})); e.value='${today}T09:00'; e.dispatchEvent(new Event('input',{bubbles:true})) })()`)
  await clickText('button', '保存')
  await waitFor(`document.body.innerText.includes('结束时间必须晚于开始时间')`, 'end before start error')
  check('end-before-start error shown on the field', true)
  await evaluate(`(() => { const [s,e] = document.querySelectorAll('input[type=datetime-local]'); s.value='${today}T23:00'; s.dispatchEvent(new Event('input',{bubbles:true})); e.value='${tomorrow}T01:00'; e.dispatchEvent(new Event('input',{bubbles:true})) })()`)
  await clickText('button', '保存')
  await waitFor(`document.body.innerText.includes('E2E 夜间实验') && !document.querySelector('input[type=datetime-local]')`, 'event saved')
  const badgeToday = await evaluate(`document.querySelector('.day.today .badge.event')?.innerText`)
  const tomorrowCell = `[...document.querySelectorAll('.day')].find(d => d.getAttribute('aria-label') && d.classList.contains('today')).nextElementSibling`
  const badgeTomorrow = await evaluate(`(${tomorrowCell})?.querySelector('.badge.event')?.innerText`)
  check('overnight event shown on today and tomorrow', badgeToday === '行程 1' && badgeTomorrow === '行程 1', `${badgeToday} / ${badgeTomorrow}`)
  check('overnight event time shows the next date', (await textOf('.items .time')).includes('23:00 – '))

  // Meal with unknown calories and zero protein.
  await clickText('button', '+ 新增记录')
  await waitFor(`!!document.querySelector('select')`, 'meal form')
  await fill('select', 'lunch')
  await fill('.form input[maxlength="200"]', 'E2E 鸡胸肉沙拉')
  await evaluate(`(() => { const ins = document.querySelectorAll('.nutrients input'); ins[1].value='0'; ins[1].dispatchEvent(new Event('input',{bubbles:true})) })()`)
  await clickText('button', '保存')
  await waitFor(`document.body.innerText.includes('E2E 鸡胸肉沙拉') && !document.querySelector('select')`, 'meal saved')
  const nutrients = await textOf('.meal-group .nutrients')
  check('blank calories shown as 未填写, zero protein as 0', nutrients.includes('热量 未填写') && nutrients.includes('蛋白质 0 g'), nutrients.replace(/\n/g, ' | '))

  // Edit the meal.
  await evaluate(`[...document.querySelectorAll('.meal-group button.link')].find(b => b.innerText==='编辑').click()`)
  await waitFor(`!!document.querySelector('select')`, 'meal edit form')
  check('edit form keeps blank nutrition blank', (await evaluate(`document.querySelectorAll('.nutrients input')[0].value`)) === '')
  await fill('.form input[maxlength="200"]', 'E2E 鸡胸肉沙拉（大份）')
  await clickText('button', '保存')
  await waitFor(`document.body.innerText.includes('E2E 鸡胸肉沙拉（大份）') && !document.querySelector('select')`, 'meal edited')
  check('meal edited', true)

  // Persistence: reload. Page.reload returns before the new document exists, so
  // mark the old one and wait until the mark is gone and the data is rendered.
  await evaluate('window.__beforeReload = true')
  await send('Page.reload')
  await waitFor(`!window.__beforeReload && document.querySelectorAll('.items button.link.danger').length >= 2 &&
    document.body.innerText.includes('E2E 夜间实验') && document.body.innerText.includes('（大份）')`, 'data after reload')
  check('data still there after reload', true)
  await click('.fab')
  await waitFor(`!!document.querySelector('.panel')`, 'panel after reload')
  check('reload starts a new conversation', await evaluate(`!!${q('.panel .intro')} && !document.querySelector('.panel .bubble')`))
  await click('.panel .close')

  // Delete the event (accept the confirm dialog).
  await evaluate(`window.confirm = () => true`)
  await evaluate(`[...document.querySelectorAll('.items button.link.danger')][0].click()`)
  await waitFor(`!document.body.innerText.includes('E2E 夜间实验')`, 'event deleted')
  check('event deleted after confirmation', (await bodyText()).includes('这一天还没有行程'))

  // Profile page shows the saved values.
  await evaluate(`[...document.querySelectorAll('nav a')].find(a => a.innerText==='Profile').click()`)
  await waitFor(`location.pathname === '/profile' && !!document.querySelector('input[placeholder^="例如：花生"]')`, 'profile with allergens')
  check('profile keeps allergens', (await evaluate(`${q('input[placeholder^="例如：花生"]')}.value`)) === '花生、虾')

  // CSRF: every write after registration used the session token, none failed.
  const writes = requests.filter((r) => r.method !== 'GET')
  const afterLogin = writes.filter((r) => !r.url.startsWith('/api/auth/register'))
  check('all writes after login used the rotated session token, no retries',
    afterLogin.length > 0 && afterLogin.every((r) => r.csrf === sessionCsrf) && !requests.some((r) => r.status === 403),
    `${afterLogin.length} writes, statuses ${[...new Set(writes.map((r) => r.status))].join(',')}`)
  check('writes carry the browser Origin', writes.length > 0 && writes.every((r) => r.origin === BASE), [...new Set(writes.map((r) => r.origin))].join(','))

  // Logout: old session cookie stops working.
  const oldSession = await getCookie('mogumogu_session')
  await clickText('button', '退出')
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
