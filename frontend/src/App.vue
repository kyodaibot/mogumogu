<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { session } from './session'
import { errorMessage } from './lib/messages'

const router = useRouter()
const loggingOut = ref(false)
const logoutError = ref('')

async function logout() {
  loggingOut.value = true
  logoutError.value = ''
  try {
    await session.logout()
    await router.push('/login')
  } catch (err) {
    logoutError.value = errorMessage(err)
  } finally {
    loggingOut.value = false
  }
}
</script>

<template>
  <header class="app-header">
    <RouterLink to="/" class="brand">mogumogu</RouterLink>
    <nav v-if="session.state.user">
      <RouterLink v-if="session.state.hasProfile" to="/">日历</RouterLink>
      <RouterLink to="/profile">Profile</RouterLink>
      <span class="username">{{ session.state.user.username }}</span>
      <button type="button" class="link" :disabled="loggingOut" @click="logout">
        {{ loggingOut ? '退出中…' : '退出' }}
      </button>
    </nav>
  </header>
  <p v-if="logoutError" class="banner error" role="alert">{{ logoutError }}</p>
  <RouterView />
</template>

<style scoped>
.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.75rem 1rem;
  border-bottom: 1px solid var(--border);
}

.brand {
  font-weight: 700;
  font-size: 1.1rem;
  color: inherit;
  text-decoration: none;
}

nav {
  display: flex;
  align-items: center;
  gap: 1rem;
}

nav a {
  color: inherit;
  text-decoration: none;
}

nav a.router-link-exact-active {
  font-weight: 600;
  text-decoration: underline;
}

.username {
  color: var(--muted);
  font-size: 0.9rem;
}
</style>
