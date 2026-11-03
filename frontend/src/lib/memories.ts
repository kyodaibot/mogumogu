// Memories (GLOSSARY: 记忆) on the Profile page and under replies.

import { ApiError, api } from '../api'
import { t } from '../i18n'
import { errorMessage } from './messages'

/** The API's limits (docs/api.md 10). */
export const MAX_MEMORIES = 30
export const MAX_MEMORY_CHARS = 100

/** What to say when adding, editing or deleting a memory failed. */
export function memoryErrorText(err: unknown): string {
  const m = t.value.memories
  if (err instanceof ApiError) {
    if (err.code === 'pro_required') return m.proOnly
    switch (err.details[0]?.reason) {
      case 'duplicate_item':
        return m.duplicate
      case 'limit_reached':
        return m.full(MAX_MEMORIES)
      case 'too_long':
      case 'invalid_format':
        return m.tooLong(MAX_MEMORY_CHARS)
    }
  }
  return errorMessage(err)
}

/** Deletes a memory; one already gone (deleted elsewhere) counts as deleted. */
export async function deleteMemoryIdempotent(id: number, del: (id: number) => Promise<void> = api.deleteMemory) {
  try {
    await del(id)
  } catch (err) {
    if (!(err instanceof ApiError && err.code === 'not_found')) throw err
  }
}
