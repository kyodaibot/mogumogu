// Every documented error code, and the client's own, has text in both
// languages (src/i18n errors.codes). Only an unexpected response falls back
// to "request failed (status)".
import { expect, it } from 'vitest'
import { en } from '../i18n/en'
import { ja } from '../i18n/ja'

// A glob, not an import: docs/ is outside frontend/, which is all the image's
// build copies, and a glob type-checks without the file.
const docs = import.meta.glob<string>('../../../docs/api.md', { eager: true, query: '?raw', import: 'default' })
const apiDoc = Object.values(docs)[0]

// The table in docs/api.md 1.6: | 404 | `not_found` | … |
const documented = [...(apiDoc ?? '').matchAll(/^\| \d{3} \| `([a-z_]+)` \|/gm)].map((m) => m[1])
// Made by the API client itself (src/api/client.ts), not by the server.
const clientSide = ['network_error', 'server_unavailable', 'timeout']

it('reads the error table of docs/api.md', () => {
  expect(documented).toContain('validation_failed')
  expect(documented.length).toBeGreaterThan(10)
})

it.each([...documented, ...clientSide])('has text for %s', (code) => {
  expect(en.errors.codes[code]).toBeTruthy()
  expect(ja.errors.codes[code]).toBeTruthy()
})
