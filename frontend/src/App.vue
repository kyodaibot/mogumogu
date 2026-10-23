<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

// Development status page: shows backend liveness and database readiness.
// It will be replaced by the real pages in phase 2.

type CheckState =
  | { kind: 'loading' }
  | { kind: 'ok' }
  | { kind: 'down'; httpStatus: number }
  | { kind: 'error'; message: string }

interface Check {
  label: string
  endpoint: string
  okText: string
  downText: string
  state: CheckState
}

const REQUEST_TIMEOUT_MS = 5000

const checks = reactive<Check[]>([
  {
    label: '后端',
    endpoint: '/api/health/live',
    okText: '正常',
    downText: '异常',
    state: { kind: 'loading' },
  },
  {
    label: '数据库',
    endpoint: '/api/health/ready',
    okText: '正常',
    downText: '不可用',
    state: { kind: 'loading' },
  },
])

const checkedAt = ref<Date | null>(null)
const refreshing = ref(false)

async function runCheck(endpoint: string): Promise<CheckState> {
  try {
    const res = await fetch(endpoint, {
      cache: 'no-store',
      signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
    })
    if (res.ok) return { kind: 'ok' }
    // 503 is the backend's own "not ready" answer. Anything else (for example
    // the 5xx the Vite proxy returns when the backend is down) is a failure
    // to get an answer at all.
    if (res.status === 503) return { kind: 'down', httpStatus: res.status }
    return { kind: 'error', message: `请求失败（HTTP ${res.status}）` }
  } catch (err) {
    const timedOut = err instanceof DOMException && err.name === 'TimeoutError'
    return { kind: 'error', message: timedOut ? '请求超时' : '请求失败（无法连接）' }
  }
}

async function refresh() {
  refreshing.value = true
  for (const check of checks) check.state = { kind: 'loading' }

  await Promise.all(
    checks.map(async (check) => {
      check.state = await runCheck(check.endpoint)
    }),
  )

  checkedAt.value = new Date()
  refreshing.value = false
}

function statusText(check: Check): string {
  switch (check.state.kind) {
    case 'loading':
      return '检查中…'
    case 'ok':
      return check.okText
    case 'down':
      return `${check.downText}（HTTP ${check.state.httpStatus}）`
    case 'error':
      return check.state.message
  }
}

onMounted(refresh)
</script>

<template>
  <main>
    <h1>mogumogu 开发状态</h1>

    <ul class="checks">
      <li v-for="check in checks" :key="check.endpoint" :class="['check', check.state.kind]">
        <span class="label">{{ check.label }}</span>
        <span class="status" role="status">{{ statusText(check) }}</span>
        <code class="endpoint">{{ check.endpoint }}</code>
      </li>
    </ul>

    <div class="actions">
      <button type="button" :disabled="refreshing" @click="refresh">
        {{ refreshing ? '刷新中…' : '刷新' }}
      </button>
      <span v-if="checkedAt" class="checked-at">
        上次检查：{{ checkedAt.toLocaleTimeString() }}
      </span>
    </div>
  </main>
</template>

<style scoped>
main {
  max-width: 32rem;
  margin: 3rem auto;
  padding: 0 1rem;
}

.checks {
  list-style: none;
  padding: 0;
  display: grid;
  gap: 0.75rem;
}

.check {
  display: grid;
  grid-template-columns: 5rem 1fr;
  gap: 0.25rem 1rem;
  padding: 0.75rem 1rem;
  border: 1px solid var(--border);
  border-left-width: 6px;
  border-radius: 6px;
}

.check.loading { border-left-color: var(--muted); }
.check.ok { border-left-color: var(--ok); }
.check.down,
.check.error { border-left-color: var(--bad); }

.label { font-weight: 600; }
.check.ok .status { color: var(--ok); }
.check.down .status,
.check.error .status { color: var(--bad); }

.endpoint {
  grid-column: 2;
  font-size: 0.8rem;
  color: var(--muted);
}

.actions {
  display: flex;
  align-items: center;
  gap: 1rem;
}

.checked-at {
  font-size: 0.85rem;
  color: var(--muted);
}
</style>
