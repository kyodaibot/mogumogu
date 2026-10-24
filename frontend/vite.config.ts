import { fileURLToPath } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig, loadEnv } from 'vite'

// The repo-root .env is shared with compose and the backend. Only the
// API_PROXY_ prefix is loaded here, and it is used by the dev server config,
// never exposed to browser code.
const repoRoot = fileURLToPath(new URL('..', import.meta.url))

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, repoRoot, 'API_PROXY_')
  const apiTarget = env.API_PROXY_TARGET || 'http://localhost:8080'

  return {
    plugins: [vue()],
    server: {
      proxy: {
        '/api': { target: apiTarget },
      },
    },
  }
})
