import { beforeEach, describe, expect, it } from 'vitest'
import { setLocale } from '../i18n'
import { clearChat, loadChat, saveChat, type ChatTurn } from './chatStore'

function memoryStore() {
  const items = new Map<string, string>()
  return {
    items,
    getItem: (k: string) => items.get(k) ?? null,
    setItem: (k: string, v: string) => void items.set(k, v),
    removeItem: (k: string) => void items.delete(k),
  }
}

let store: ReturnType<typeof memoryStore>
const use = () => store

const turn = (id: number, over: Partial<ChatTurn> = {}): ChatTurn => ({
  id, date: '2026-11-02', text: `質問${id}`, status: 'done', undone: [],
  reply: { reply: '回答', recommendations: [], ask_meal_type: false, meal_type: null, proposal: null, memory_updates: [], sources: [] },
  ...over,
})

beforeEach(() => {
  store = memoryStore()
  setLocale('ja')
})

describe('chatStore', () => {
  it('gives back what was saved for the same user', () => {
    const chat = { turns: [turn(1), turn(2, { added: [0], undone: [8] })], nextId: 3 }
    saveChat(7, chat, use)

    expect(loadChat(7, use)).toEqual(chat)
  })

  it('starts empty without a saved chat, for another user, an older version or broken data', () => {
    expect(loadChat(7, use)).toEqual({ turns: [], nextId: 1 })

    saveChat(7, { turns: [turn(1)], nextId: 2 }, use)
    expect(loadChat(8, use).turns).toEqual([])

    store.items.set('mogumogu.chat', JSON.stringify({ version: 0, userId: 7, turns: [turn(1)], nextId: 2 }))
    expect(loadChat(7, use).turns).toEqual([])

    store.items.set('mogumogu.chat', '{not json')
    expect(loadChat(7, use).turns).toEqual([])
  })

  it('turns a message still waiting for its answer into a failure that can be retried', () => {
    saveChat(7, { turns: [turn(1), turn(2, { status: 'pending', reply: undefined })], nextId: 3 }, use)
    const [done, waiting] = loadChat(7, use).turns

    expect(done.status).toBe('done')
    expect(waiting).toMatchObject({ status: 'failed', retryable: true, error: expect.stringContaining('ページを離れました') })
  })

  it('lets a proposal card that was running be run again', () => {
    const proposal = { checked: [true], running: true, closed: false }
    saveChat(7, { turns: [turn(1, { proposal })], nextId: 2 }, use)

    expect(loadChat(7, use).turns[0].proposal).toEqual({ ...proposal, running: false })
  })

  it('gives a reply saved before sources existed an empty list', () => {
    const { sources: _, ...old } = turn(1).reply!
    store.setItem('mogumogu.chat', JSON.stringify({ version: 1, userId: 7, turns: [{ ...turn(1), reply: old }], nextId: 2 }))

    expect(loadChat(7, use).turns[0].reply!.sources).toEqual([])
  })

  it('clears the saved chat', () => {
    saveChat(7, { turns: [turn(1)], nextId: 2 }, use)
    clearChat(use)

    expect(loadChat(7, use).turns).toEqual([])
  })

  it('never throws when storage is unavailable', () => {
    const broken = () => {
      throw new Error('SecurityError')
    }

    expect(() => saveChat(7, { turns: [turn(1)], nextId: 2 }, broken)).not.toThrow()
    expect(loadChat(7, broken)).toEqual({ turns: [], nextId: 1 })
    expect(() => clearChat(broken)).not.toThrow()
  })
})
