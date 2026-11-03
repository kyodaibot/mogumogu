// The conversation (GLOSSARY: 对话) of the chat panel: its turns and every
// rule about them. Sending, retrying, which failures can be retried, the
// turn limit, the proposal cards and memory undos all change the turns only
// here, and each change is saved (lib/chatStore) when it is made. The panel
// (components/AssistantChat.vue) only shows it.

import { computed, reactive } from 'vue'
import { ApiError, api, type AssistantReply, type ChangeResult, type HistoryTurn } from '../api'
import { locale, t } from '../i18n'
import { MAX_LENGTH, MAX_TURNS, historyOf, quotaText } from './assistant'
import { loadChat, saveChat, type Chat, type ChatTurn } from './chatStore'
import type { Ymd } from './date'
import { deleteMemoryIdempotent } from './memories'
import { errorMessage } from './messages'
import { newProposalState, proposalFailure } from './proposals'

/** The calls the conversation makes; the real API unless a test gives others. */
export interface ConversationApi {
  sendMessage(date: Ymd, message: string, locale: 'ja' | 'en', history: HistoryTurn[]): Promise<AssistantReply>
  executeProposal(id: number, accepted: number[]): Promise<ChangeResult[]>
  deleteMemory(id: number): Promise<void>
}

/** Where the conversation is kept; sessionStorage unless a test gives another. */
export interface ConversationStorage {
  load(userId: number): Chat
  save(userId: number, chat: Chat): void
}

const realApi: ConversationApi = {
  sendMessage: api.sendAssistantMessage,
  executeProposal: api.executeProposal,
  deleteMemory: api.deleteMemory,
}

const sessionStore: ConversationStorage = { load: (id) => loadChat(id), save: (id, chat) => saveChat(id, chat) }

/** What the panel shows for a failed message, and what the user can do next. */
function failureOf(err: unknown): Pick<ChatTurn, 'error' | 'retryable' | 'restart'> {
  const a = t.value.assistant
  if (err instanceof ApiError) {
    if (err.code === 'quota_exceeded') return { error: quotaText(err.limit), retryable: false }
    if (err.code === 'rate_limited') return { error: a.tooFast, retryable: true }
    // timeout: no answer within ASSISTANT_TIMEOUT_MS, so the connection was lost.
    if (err.code === 'ai_unavailable' || err.code === 'timeout') return { error: a.unavailable, retryable: true }
    if (err.code === 'validation_failed') {
      // The history comes from this tab's saved copy; one Go refuses (an
      // older shape, for example) cannot be sent again, only left behind.
      if (err.details.some((d) => d.field.startsWith('history'))) {
        return { error: a.historyBroken, retryable: false, restart: true }
      }
      if (err.details.some((d) => d.field === 'message')) return { error: a.tooLong(MAX_LENGTH), retryable: false }
      return { error: errorMessage(err), retryable: false }
    }
  }
  return { error: errorMessage(err), retryable: true }
}

export type Conversation = ReturnType<typeof createConversation>

export function createConversation(
  userId: number,
  { api = realApi, storage = sessionStore }: { api?: ConversationApi; storage?: ConversationStorage } = {},
) {
  const chat = reactive<Chat>(storage.load(userId))
  const save = () => storage.save(userId, chat)
  const find = (id: number) => chat.turns.find((turn) => turn.id === id)

  const busy = computed(() => chat.turns.some((turn) => turn.status === 'pending'))
  // Ten answered messages end a conversation; the user starts a new one.
  const full = computed(() => chat.turns.filter((turn) => turn.status === 'done').length >= MAX_TURNS)

  async function ask(turn: ChatTurn) {
    // Only the last turn is ever sent or retried, so the turns before it are
    // the history.
    const history = historyOf(chat.turns.slice(0, chat.turns.indexOf(turn)))
    Object.assign(turn, { status: 'pending', error: undefined, retryable: undefined, restart: undefined })
    save()
    try {
      const reply = await api.sendMessage(turn.date, turn.text, locale.value, history)
      // A new conversation started meanwhile: this answer belongs to none.
      if (!chat.turns.includes(turn)) return
      turn.reply = reply
      if (reply.proposal) turn.proposal = newProposalState(reply.proposal)
      turn.status = 'done'
    } catch (err) {
      if (!chat.turns.includes(turn)) return
      Object.assign(turn, { status: 'failed', ...failureOf(err) })
    }
    save()
  }

  /** Only the last turn can be retried: an older one would come after
   *  later turns in the history. */
  function canRetry(turn: ChatTurn): boolean {
    return turn.status === 'failed' && !!turn.retryable && !busy.value && turn === chat.turns[chat.turns.length - 1]
  }

  return {
    /** Oldest first. Read only: change them with the functions below. */
    get turns(): readonly ChatTurn[] {
      return chat.turns
    },
    busy,
    full,

    /** Sends a message about date; null when nothing was sent (empty,
     *  waiting for an answer, or the conversation is full). The promise
     *  settles once the answer or the failure is shown. */
    send(text: string, date: Ymd): Promise<void> | null {
      text = text.trim()
      if (!text || busy.value || full.value) return null
      chat.turns.push({ id: chat.nextId++, date, text, status: 'pending', undone: [] })
      // The reactive copy, so changes to it re-render.
      return ask(chat.turns[chat.turns.length - 1])
    },

    canRetry,

    retry(turnId: number): Promise<void> | null {
      const turn = find(turnId)
      return turn && canRetry(turn) ? ask(turn) : null
    },

    /** Clears the conversation; false while an answer is still coming. */
    startNew(): boolean {
      if (busy.value) return false
      chat.turns = []
      save()
      return true
    },

    /** Recommendation index of the turn was added to the meal log. */
    markAdded(turnId: number, index: number) {
      const turn = find(turnId)
      if (!turn || turn.added?.includes(index)) return
      turn.added = [...(turn.added ?? []), index]
      save()
    },

    /** Checks or unchecks one change of the turn's proposal before it runs. */
    setChecked(turnId: number, index: number, checked: boolean) {
      const state = find(turnId)?.proposal
      if (!state || state.running || state.results || state.closed) return
      state.checked[index] = checked
      save()
    },

    /** Runs the checked changes of the turn's proposal. True when it ran, so
     *  meal records may have changed. */
    async runProposal(turnId: number): Promise<boolean> {
      const turn = find(turnId)
      const state = turn?.proposal
      const proposal = turn?.reply?.proposal
      if (!state || !proposal || state.running || state.results || state.closed) return false
      const accepted = state.checked.flatMap((checked, i) => (checked ? [i] : []))
      if (!accepted.length) return false
      Object.assign(state, { running: true, error: undefined })
      save()
      try {
        state.results = await api.executeProposal(proposal.id, accepted)
        return true
      } catch (err) {
        Object.assign(state, proposalFailure(err))
        return false
      } finally {
        state.running = false
        save()
      }
    },

    /** Deletes a memory saved with the turn's reply. Gives the error text, or
     *  null when it is undone. */
    async undoMemory(turnId: number, memoryId: number): Promise<string | null> {
      const turn = find(turnId)
      if (!turn || turn.undone.includes(memoryId)) return null
      try {
        await deleteMemoryIdempotent(memoryId, api.deleteMemory)
      } catch (err) {
        return errorMessage(err)
      }
      turn.undone.push(memoryId)
      save()
      return null
    },
  }
}
