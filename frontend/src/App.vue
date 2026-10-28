<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { session } from './session'
import { errorMessage } from './lib/messages'
import MealTray from './components/MealTray.vue'

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
    <RouterLink to="/" class="brand">
      <MealTray :filled="['breakfast', 'lunch', 'dinner', 'snack']" size="md" />
      <span class="wordmark">mogumogu</span>
    </RouterLink>
    <nav v-if="session.state.user">
      <RouterLink v-if="session.state.hasProfile" to="/">日历</RouterLink>
      <RouterLink to="/profile">Profile</RouterLink>
      <span class="account">
        <span class="username" :title="session.state.user.username">{{ session.state.user.username }}</span>
        <button type="button" class="link" :disabled="loggingOut" @click="logout">
          {{ loggingOut ? '退出中…' : '退出' }}
        </button>
      </span>
    </nav>
  </header>
  <p v-if="logoutError" class="banner error logout-error" role="alert">{{ logoutError }}</p>
  <RouterView />
</template>

<style scoped>
.app-header {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.625rem 1.25rem;
  background: color-mix(in srgb, var(--paper) 92%, transparent);
  backdrop-filter: blur(8px);
  border-bottom: 1px solid var(--line);
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: inherit;
  text-decoration: none;
  border-radius: 8px;
}

.wordmark {
  font-family: var(--font-num);
  font-weight: 800;
  font-size: 1.1875rem;
  letter-spacing: -0.02em;
}

nav {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
}

nav a {
  padding: 0.375rem 0.75rem;
  border-radius: 999px;
  color: var(--ink-soft);
  font-weight: 600;
  text-decoration: none;
  white-space: nowrap;
}

nav a:hover {
  color: var(--ink);
  background: var(--surface);
}

nav a.router-link-exact-active {
  color: var(--ink);
  background: var(--surface);
}

.account {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
  margin-left: 0.75rem;
  padding-left: 0.75rem;
  border-left: 1px solid var(--line);
}

.username {
  color: var(--ink-soft);
  font-size: var(--fs-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 12rem;
}

.account button {
  white-space: nowrap;
}

.logout-error {
  margin: 0.75rem 1.25rem;
}

@media (max-width: 560px) {
  .app-header {
    padding: 0.5rem 1rem;
    gap: 0.5rem;
  }

  .wordmark {
    font-size: 1.0625rem;
  }

  nav a {
    padding: 0.375rem 0.5rem;
  }

  .account {
    margin-left: 0.25rem;
    padding-left: 0.375rem;
  }

  .username {
    max-width: 4.5rem;
  }
}
</style>
