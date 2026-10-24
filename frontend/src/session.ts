// Who is logged in, shared across pages. Kept deliberately small instead of
// adding a state-management library.

import { reactive, readonly } from 'vue'
import { api, ApiError, type Me, type User } from './api'

const state = reactive({
  user: null as User | null,
  hasProfile: false,
  /** The startup check has finished (successfully or not). */
  loaded: false,
})

function apply(me: Me | null) {
  state.user = me?.user ?? null
  state.hasProfile = me?.has_profile ?? false
}

export const session = {
  state: readonly(state),

  /** Asks the backend who we are. A 401 simply means "logged out". */
  async load() {
    try {
      apply(await api.me({ quiet401: true }))
    } catch (err) {
      apply(null)
      if (!(err instanceof ApiError && err.status === 401)) throw err
    } finally {
      state.loaded = true
    }
  },

  async login(username: string, password: string) {
    apply(await api.login(username, password))
  },

  async register(username: string, password: string) {
    apply(await api.register(username, password))
  },

  async logout() {
    try {
      await api.logout()
    } finally {
      apply(null)
    }
  },

  markProfileSaved() {
    state.hasProfile = true
  },

  /** The session expired or was revoked elsewhere. */
  clear() {
    apply(null)
  },
}
