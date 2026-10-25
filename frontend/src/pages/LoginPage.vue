<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { session } from '../session'
import { errorMessage, fieldMessages } from '../lib/messages'
import { MEAL_TYPES } from '../lib/meals'

const route = useRoute()
const router = useRouter()

const mode = ref<'login' | 'register'>('login')
const form = reactive({ username: '', password: '' })
const submitting = ref(false)
const error = ref('')
const fields = ref<Record<string, string>>({})

const title = computed(() => (mode.value === 'login' ? '登录' : '注册'))

function switchMode(next: 'login' | 'register') {
  mode.value = next
  error.value = ''
  fields.value = {}
}

async function submit() {
  submitting.value = true
  error.value = ''
  fields.value = {}
  try {
    if (mode.value === 'login') {
      await session.login(form.username, form.password)
    } else {
      await session.register(form.username, form.password)
    }
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    await router.push(session.state.hasProfile ? redirect : '/profile')
  } catch (err) {
    fields.value = fieldMessages(err)
    error.value = errorMessage(err)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <main class="login">
    <section class="intro">
      <div class="big-tray" aria-hidden="true">
        <span v-for="t in MEAL_TYPES" :key="t.value" class="slot" :class="t.value">{{ t.label }}</span>
      </div>
      <h1>今天吃了什么？</h1>
      <p>把每天的行程和三餐记在日历上。哪一餐还没记，一眼就能看出来。</p>
    </section>

    <section class="panel">
      <div class="tabs" role="tablist">
        <button type="button" role="tab" :aria-selected="mode === 'login'" :class="{ active: mode === 'login' }" @click="switchMode('login')">登录</button>
        <button type="button" role="tab" :aria-selected="mode === 'register'" :class="{ active: mode === 'register' }" @click="switchMode('register')">注册</button>
      </div>

      <form class="card form" :aria-label="title" novalidate @submit.prevent="submit">
        <label>
          用户名
          <input v-model="form.username" name="username" autocomplete="username" required />
          <small v-if="mode === 'register' && !fields.username" class="hint">3–32 个字符，字母、数字或下划线，不区分大小写</small>
          <small v-if="fields.username" class="field-error">{{ fields.username }}</small>
        </label>

        <label>
          密码
          <input
            v-model="form.password"
            type="password"
            name="password"
            :autocomplete="mode === 'login' ? 'current-password' : 'new-password'"
            required
          />
          <small v-if="mode === 'register' && !fields.password" class="hint">8–128 个字符</small>
          <small v-if="fields.password" class="field-error">{{ fields.password }}</small>
        </label>

        <p v-if="error" class="banner error" role="alert">{{ error }}</p>

        <button type="submit" class="primary" :disabled="submitting">
          {{ submitting ? '请稍候…' : title }}
        </button>
      </form>
    </section>
  </main>
</template>

<style scoped>
.login {
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) minmax(0, 26rem);
  align-items: center;
  gap: 4rem;
  max-width: 64rem;
  min-height: calc(100vh - 4rem);
  margin: 0 auto;
  padding: 2rem 1.5rem;
  box-sizing: border-box;
}

.intro h1 {
  font-size: var(--fs-display);
  letter-spacing: -0.01em;
  margin: 2rem 0 0.75rem;
}

.intro p {
  max-width: 24em;
  margin: 0;
  color: var(--ink-soft);
  font-size: 1.0625rem;
}

/* The tray fills one slot at a time when the page opens. */
.big-tray {
  display: inline-grid;
  grid-template-columns: repeat(2, 7.5rem);
  grid-auto-rows: 5.5rem;
  gap: 0.5rem;
  padding: 0.5rem;
  border-radius: 1.5rem;
  background: var(--tray);
}

.slot {
  display: flex;
  align-items: flex-end;
  padding: 0.625rem 0.875rem;
  border-radius: 1rem;
  font-weight: 700;
  color: var(--on-meal-light);
  animation: fill 0.45s ease-out backwards;
}

.slot.breakfast { background: var(--meal-breakfast); animation-delay: 0.15s; }
.slot.lunch { background: var(--meal-lunch); color: var(--on-meal-dark); animation-delay: 0.35s; }
.slot.dinner { background: var(--meal-dinner); color: var(--on-meal-dark); animation-delay: 0.55s; }
.slot.snack { background: var(--meal-snack); animation-delay: 0.75s; }

@keyframes fill {
  from {
    background: var(--tray-slot);
    color: transparent;
    transform: scale(0.92);
  }
}

@media (prefers-reduced-motion: reduce) {
  .slot {
    animation: none;
  }
}

.tabs {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.25rem;
  padding: 0.25rem;
  margin-bottom: 1rem;
  border-radius: calc(var(--radius-control) + 0.25rem);
  background: var(--surface);
}

.tabs button {
  border-color: transparent;
  background: none;
  color: var(--ink-soft);
}

.tabs button.active {
  background: var(--paper);
  border-color: var(--line);
  color: var(--ink);
}

.form button[type='submit'] {
  padding: 0.75rem 1rem;
}

@media (max-width: 820px) {
  .login {
    grid-template-columns: minmax(0, 1fr);
    align-content: start;
    gap: 2rem;
    min-height: 0;
    max-width: 30rem;
    padding: 2rem 1.25rem 3rem;
  }

  .intro {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    column-gap: 1.25rem;
    align-items: center;
  }

  .big-tray {
    grid-row: span 2;
    grid-template-columns: repeat(2, 2.75rem);
    grid-auto-rows: 2.75rem;
    gap: 0.25rem;
    padding: 0.25rem;
    border-radius: 0.875rem;
  }

  .slot {
    padding: 0;
    justify-content: center;
    align-items: center;
    font-size: 0.75rem;
    border-radius: 0.625rem;
  }

  .intro h1 {
    font-size: 1.75rem;
    margin: 0 0 0.25rem;
  }

  .intro p {
    font-size: 0.9375rem;
  }
}
</style>
