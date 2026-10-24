<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { session } from '../session'
import { errorMessage, fieldMessages } from '../lib/messages'

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
  <main class="page narrow">
    <div class="tabs" role="tablist">
      <button type="button" role="tab" :aria-selected="mode === 'login'" :class="{ active: mode === 'login' }" @click="switchMode('login')">登录</button>
      <button type="button" role="tab" :aria-selected="mode === 'register'" :class="{ active: mode === 'register' }" @click="switchMode('register')">注册</button>
    </div>

    <form class="card form" novalidate @submit.prevent="submit">
      <h1>{{ title }}</h1>

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
  </main>
</template>

<style scoped>
.tabs {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 1rem;
}

.tabs button.active {
  border-color: var(--accent);
  color: var(--accent);
  font-weight: 600;
}
</style>
