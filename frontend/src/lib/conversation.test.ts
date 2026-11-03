import { beforeEach, describe, expect, it } from 'vitest'
import { ApiError, type AssistantReply, type ChangeResult, type Proposal } from '../api'
import { setLocale } from '../i18n'
import { loadChat, saveChat, type Chat } from './chatStore'
import { createConversation, type ConversationApi } from './conversation'

const DAY = '2026-11-03'

const reply = (text = '回答です。', over: Partial<AssistantReply> = {}): AssistantReply => ({
  reply: text, recommendations: [], ask_meal_type: false, meal_type: null, proposal: null,
  memory_updates: [], sources: [], ...over,
})

const proposal: Proposal = {
  id: 5, expires_at: '2026-11-03T14:00:00+09:00',
  changes: [
    { op: 'create', before: null, after: { date: DAY, meal_type: 'lunch', content: 'ラーメン', calories: null, protein_g: null, fat_g: null, carbs_g: null } },
    { op: 'delete', before: { id: 9, date: DAY, meal_type: 'dinner', content: 'カレー', calories: null, protein_g: null, fat_g: null, carbs_g: null }, after: null },
  ],
}

/** A fake API: each call takes the next answer (a value, or an error to throw). */
function fakeApi() {
  const calls: { name: string; args: unknown[] }[] = []
  const answers: unknown[] = []
  let release: (() => void) | null = null
  const next = async (name: string, ...args: unknown[]) => {
    calls.push({ name, args })
    if (release === null && answers[0] === HOLD) {
      answers.shift()
      await new Promise<void>((r) => (release = r))
      release = null
    }
    const a = answers.shift()
    if (a instanceof Error) throw a
    return a
  }
  const api: ConversationApi = {
    sendMessage: (...args) => next('send', ...args) as Promise<AssistantReply>,
    executeProposal: (...args) => next('execute', ...args) as Promise<ChangeResult[]>,
    deleteMemory: (...args) => next('delete', ...args) as Promise<void>,
  }
  return { api, calls, answers, release: () => release?.() }
}
const HOLD = Symbol('hold')

/** Keeps every save as JSON, as sessionStorage does. */
function memoryStorage(saved?: Chat) {
  const saves: Chat[] = []
  const copy = (chat: Chat): Chat => JSON.parse(JSON.stringify(chat))
  return {
    saves,
    load: () => copy(saved ?? { turns: [], nextId: 1 }),
    save: (_: number, chat: Chat) => void saves.push(copy(chat)),
  }
}

const err = (code: string, details: { field: string; reason: string }[] = [], limit?: number) =>
  new ApiError(code === 'quota_exceeded' ? 429 : 503, code, code, details, limit)

let fake: ReturnType<typeof fakeApi>
let storage: ReturnType<typeof memoryStorage>
const start = (saved?: Chat) => {
  storage = memoryStorage(saved)
  return createConversation(7, { api: fake.api, storage })
}

beforeEach(() => {
  fake = fakeApi()
  setLocale('ja')
})

describe('sending', () => {
  it('sends with the history, the locale and the date, and saves the answer', async () => {
    const c = start()
    fake.answers.push(reply('一つ目'), reply('二つ目'))
    await c.send('昼は？', DAY)
    setLocale('en')
    await c.send('  and dinner?  ', '2026-11-04')

    expect(fake.calls[1].args).toEqual([
      '2026-11-04', 'and dinner?', 'en',
      [{ date: DAY, message: '昼は？', reply: '一つ目', recommendations: [], changes: [] }],
    ])
    expect(c.turns.map((t) => [t.id, t.status, t.reply?.reply])).toEqual([[1, 'done', '一つ目'], [2, 'done', '二つ目']])
    expect(storage.saves.at(-1)).toEqual({ turns: c.turns, nextId: 3 })
  })

  it('sends nothing empty, while waiting for an answer, or when the conversation is full', async () => {
    const c = start()
    expect(c.send('   ', DAY)).toBeNull()

    fake.answers.push(HOLD, reply())
    const sending = c.send('一つ目', DAY)!
    expect(c.busy.value).toBe(true)
    expect(c.send('二つ目', DAY)).toBeNull()
    expect(c.startNew()).toBe(false)
    fake.release()
    await sending

    for (let i = 1; i < 10; i++) {
      fake.answers.push(reply())
      await c.send(`質問${i}`, DAY)
    }
    expect(c.full.value).toBe(true)
    expect(c.send('11', DAY)).toBeNull()
    expect(fake.calls).toHaveLength(10)
  })

  it('makes a card state for a reply with a proposal', async () => {
    const c = start()
    fake.answers.push(reply('カードで確認してください。', { proposal }))
    await c.send('お昼にラーメン食べた', DAY)

    expect(c.turns[0].proposal).toEqual({ checked: [true, true], running: false, closed: false })
  })

  it('leaves out failed turns from the history', async () => {
    const c = start()
    fake.answers.push(err('ai_unavailable'), reply())
    await c.send('一つ目', DAY)
    await c.send('二つ目', DAY)

    expect(fake.calls[1].args[3]).toEqual([])
  })
})

describe('failures', () => {
  it.each([
    ['ai_unavailable', [], '食事アシスタントが回答できませんでした。もう一度お試しください。', true],
    ['timeout', [], '食事アシスタントが回答できませんでした。もう一度お試しください。', true],
    ['rate_limited', [], '短い時間に送信しすぎです。少し待ってから再試行してください。', true],
    ['network_error', [], 'サーバーに接続できません。ネットワークを確認するか、しばらくしてからお試しください。', true],
    ['quota_exceeded', [], '今日の質問回数を使い切りました。また明日どうぞ。', false],
    ['validation_failed', [{ field: 'message', reason: 'too_long' }], 'メッセージは500文字までです。', false],
  ] as const)('%s', async (code, details, text, retryable) => {
    const c = start()
    fake.answers.push(err(code, [...details]))
    await c.send('質問', DAY)

    expect(c.turns[0]).toMatchObject({ status: 'failed', error: text, retryable })
    expect(c.canRetry(c.turns[0])).toBe(retryable)
    expect(c.turns[0].restart).toBeUndefined()
  })

  it('tells the user\'s own quota', async () => {
    const c = start()
    fake.answers.push(err('quota_exceeded', [], 30))
    await c.send('質問', DAY)

    expect(c.turns[0].error).toContain('30')
  })

  it('offers a new conversation when Go refuses the history', async () => {
    const c = start()
    fake.answers.push(err('validation_failed', [{ field: 'history[0].reply', reason: 'too_long' }]))
    await c.send('質問', DAY)

    expect(c.turns[0]).toMatchObject({
      error: 'この会話はこれ以上続けられません。新しい会話を始めてください。', retryable: false, restart: true,
    })
  })

  it('retries only the last turn, and clears the failure', async () => {
    const c = start()
    fake.answers.push(err('ai_unavailable'), err('ai_unavailable'))
    await c.send('一つ目', DAY)
    await c.send('二つ目', DAY)
    const [first, last] = c.turns

    expect(c.canRetry(first)).toBe(false)
    expect(c.retry(first.id)).toBeNull()

    fake.answers.push(reply('答え'))
    await c.retry(last.id)
    expect(c.turns[1]).toMatchObject({ status: 'done', error: undefined, retryable: undefined })
    expect(fake.calls.at(-1)!.args.slice(0, 2)).toEqual([DAY, '二つ目'])
  })
})

describe('a new conversation', () => {
  it('clears the turns and keeps counting ids', async () => {
    const c = start()
    fake.answers.push(reply(), reply())
    await c.send('一つ目', DAY)
    expect(c.startNew()).toBe(true)
    expect(storage.saves.at(-1)!.turns).toEqual([])

    await c.send('新しい', DAY)
    expect(c.turns.map((t) => t.id)).toEqual([2])
    expect(fake.calls[1].args[3]).toEqual([])
  })
})

describe('proposals', () => {
  async function withProposal() {
    const c = start()
    fake.answers.push(reply('カードで確認してください。', { proposal }))
    await c.send('お昼にラーメン食べた', DAY)
    return c
  }

  it('runs the checked changes and keeps the results', async () => {
    const c = await withProposal()
    c.setChecked(1, 1, false)
    const results: ChangeResult[] = [{ status: 'done' }, { status: 'skipped' }]
    fake.answers.push(results)

    expect(await c.runProposal(1)).toBe(true)
    expect(fake.calls.at(-1)).toEqual({ name: 'execute', args: [5, [0]] })
    expect(c.turns[0].proposal).toEqual({ checked: [true, false], running: false, closed: false, results, error: undefined })
    expect(storage.saves.at(-1)!.turns[0].proposal!.results).toEqual(results)

    // Locked once it ran.
    c.setChecked(1, 1, true)
    expect(await c.runProposal(1)).toBe(false)
    expect(c.turns[0].proposal!.checked).toEqual([true, false])
  })

  it('runs nothing with no change checked', async () => {
    const c = await withProposal()
    c.setChecked(1, 0, false)
    c.setChecked(1, 1, false)

    expect(await c.runProposal(1)).toBe(false)
    expect(fake.calls.map((x) => x.name)).toEqual(['send'])
  })

  it('can run again after a failure a retry may fix', async () => {
    const c = await withProposal()
    fake.answers.push(err('network_error'))

    expect(await c.runProposal(1)).toBe(false)
    expect(c.turns[0].proposal).toMatchObject({ running: false, closed: false })
    expect(c.turns[0].proposal!.error).toContain('サーバーに接続できません')

    fake.answers.push([{ status: 'done' }, { status: 'done' }])
    expect(await c.runProposal(1)).toBe(true)
    expect(c.turns[0].proposal!.error).toBeUndefined()
  })

  it.each(['proposal_executed', 'proposal_expired', 'pro_required', 'not_found'])('closes the card on %s', async (code) => {
    const c = await withProposal()
    fake.answers.push(err(code))

    expect(await c.runProposal(1)).toBe(false)
    expect(c.turns[0].proposal).toMatchObject({ running: false, closed: true })
    expect(await c.runProposal(1)).toBe(false)
  })
})

describe('memory undo', () => {
  async function withMemory() {
    const c = start()
    fake.answers.push(reply('覚えました。', { memory_updates: [{ op: 'create', id: 4, content: '辛いものが苦手', before: null }] }))
    await c.send('辛いのは苦手', DAY)
    return c
  }

  it('deletes the memory and keeps it undone', async () => {
    const c = await withMemory()
    fake.answers.push(undefined)

    expect(await c.undoMemory(1, 4)).toBeNull()
    expect(c.turns[0].undone).toEqual([4])
    expect(storage.saves.at(-1)!.turns[0].undone).toEqual([4])
  })

  it('counts a memory already deleted elsewhere as undone', async () => {
    const c = await withMemory()
    fake.answers.push(new ApiError(404, 'not_found', 'gone'))

    expect(await c.undoMemory(1, 4)).toBeNull()
    expect(c.turns[0].undone).toEqual([4])
  })

  it('gives the error otherwise, and keeps the memory', async () => {
    const c = await withMemory()
    fake.answers.push(err('network_error'))

    expect(await c.undoMemory(1, 4)).toContain('サーバーに接続できません')
    expect(c.turns[0].undone).toEqual([])
  })
})

describe('kept in the tab', () => {
  it('goes on after a reload, with the interrupted message to retry', async () => {
    const items = new Map<string, string>()
    const tab = () => ({
      getItem: (k: string) => items.get(k) ?? null,
      setItem: (k: string, v: string) => void items.set(k, v),
      removeItem: (k: string) => void items.delete(k),
    })
    const inTab = { load: (id: number) => loadChat(id, tab), save: (id: number, chat: Chat) => saveChat(id, chat, tab) }
    const before = createConversation(7, { api: fake.api, storage: inTab })
    fake.answers.push(reply('答え'), HOLD)
    await before.send('一つ目', DAY)
    void before.send('二つ目', DAY) // the page is left before its answer

    const after = createConversation(7, { api: fake.api, storage: inTab })
    expect(after.turns.map((t) => t.status)).toEqual(['done', 'failed'])
    expect(after.canRetry(after.turns[1])).toBe(true)

    fake.answers.push(reply('二つ目の答え'))
    await after.retry(2)
    expect(fake.calls.at(-1)!.args[3]).toHaveLength(1)
    expect(after.turns[1].reply!.reply).toBe('二つ目の答え')
  })

  it('remembers which recommendations were added', async () => {
    const c = start()
    fake.answers.push(reply())
    await c.send('一つ目', DAY)
    c.markAdded(1, 2)
    c.markAdded(1, 2)

    expect(c.turns[0].added).toEqual([2])
    expect(storage.saves.at(-1)!.turns[0].added).toEqual([2])
  })
})
