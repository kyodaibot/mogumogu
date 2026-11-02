// The conversation kept in sessionStorage (plans/archive/chat-session-storage.md), so
// it survives a reload and leaving the calendar page, and ends with the tab.
// The server still keeps no conversation; this copy only lives in the browser.

import { t } from '../i18n'
import type { SentTurn } from './assistant'
import type { ProposalState } from './proposals'

/** One message of the chat panel and what came back. */
export interface ChatTurn extends SentTurn {
  id: number
  error?: string
  retryable?: boolean
  /** Indexes of recommendations already added to the meal log. */
  added?: number[]
  /** The state of the reply's change proposal card, if it has one. */
  proposal?: ProposalState
  /** Ids of memories saved with the reply that the user undid. */
  undone: number[]
}

export interface Chat {
  turns: ChatTurn[]
  /** The id for the next turn. */
  nextId: number
}

type Store = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

const KEY = 'mogumogu.chat'
// Raised when ChatTurn changes shape; an older copy is then dropped.
const VERSION = 1

// sessionStorage can be missing, blocked or full (private windows, settings);
// every access is guarded and the chat then simply ends with the page.
const browserStore = (): Store => globalThis.sessionStorage

const empty = (): Chat => ({ turns: [], nextId: 1 })

export function saveChat(userId: number, chat: Chat, store: () => Store = browserStore) {
  try {
    store().setItem(KEY, JSON.stringify({ version: VERSION, userId, ...chat }))
  } catch {
    // Not kept; the chat itself goes on.
  }
}

/** The saved conversation of this user, or an empty one. */
export function loadChat(userId: number, store: () => Store = browserStore): Chat {
  try {
    const saved = JSON.parse(store().getItem(KEY) ?? 'null')
    // Another user's chat (a shared computer) or an older shape is dropped.
    if (saved?.version !== VERSION || saved.userId !== userId || !Array.isArray(saved.turns)) return empty()
    return { turns: saved.turns.map(resume), nextId: saved.nextId }
  } catch {
    return empty()
  }
}

export function clearChat(store: () => Store = browserStore) {
  try {
    store().removeItem(KEY)
  } catch {
    // Nothing to clear.
  }
}

/** A turn after a reload: what was still in flight did not finish here. */
function resume(turn: ChatTurn): ChatTurn {
  if (turn.status === 'pending') {
    return { ...turn, status: 'failed', error: t.value.assistant.interrupted, retryable: true }
  }
  // The proposal may or may not have run; running it again answers 409,
  // which the card shows.
  if (turn.proposal?.running) return { ...turn, proposal: { ...turn.proposal, running: false } }
  return turn
}
