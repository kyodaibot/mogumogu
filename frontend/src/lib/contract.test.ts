// The limits the browser shares with Go and the AI service (contract/README.md).
// When one fails, change contract/limits.json and the other tiers too.
import { expect, it } from 'vitest'
import { MAX_LENGTH, MAX_TURNS } from './assistant'
import { MAX_MEMORIES, MAX_MEMORY_CHARS } from './memories'

// A glob, not an import: the contract is outside frontend/, which is all the
// image's build copies, and a glob type-checks without the file.
const files = import.meta.glob<Record<string, unknown>>('../../../contract/limits.json', { eager: true, import: 'default' })
const limits = Object.values(files)[0]

it('reads the contract', () => {
  expect(limits).toBeDefined()
})

it('has the limits of the contract', () => {
  expect(MAX_LENGTH).toBe(limits.message_chars)
  // The history holds every turn but the one being sent.
  expect(MAX_TURNS).toBe((limits.history_turns as number) + 1)
  expect(MAX_MEMORIES).toBe(limits.memories)
  expect(MAX_MEMORY_CHARS).toBe(limits.memory_chars)
})
