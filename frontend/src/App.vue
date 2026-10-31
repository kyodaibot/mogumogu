<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { session } from './session'
import { errorMessage } from './lib/messages'
import LocaleSwitch from './components/LocaleSwitch.vue'
import MealTray from './components/MealTray.vue'
import { t } from './i18n'

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
      <RouterLink v-if="session.state.hasProfile" to="/">{{ t.nav.calendar }}</RouterLink>
      <RouterLink to="/profile">{{ t.nav.profile }}</RouterLink>
    </nav>
    <div class="tools">
      <span v-if="session.state.user" class="account">
        <span class="username" :title="session.state.user.username">{{ session.state.user.username }}</span>
        <span v-if="session.state.user.tier === 'pro'" class="tier" :title="t.nav.proTitle">{{ t.nav.pro }}</span>
        <button type="button" class="link" :disabled="loggingOut" @click="logout">
          {{ loggingOut ? t.nav.loggingOut : t.nav.logout }}
        </button>
      </span>
      <LocaleSwitch />
    </div>
  </header>
  <p v-if="logoutError" class="banner error logout-error" role="alert">{{ logoutError }}</p>
  <RouterView />
</template>

<style scoped>
/* One row on wide screens: brand, then nav and tools on the right. On phones
   the nav links move to a second row, so Japanese and English labels fit. */
.app-header {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.625rem 1.25rem;
  background: color-mix(in srgb, var(--paper) 92%, transparent);
  backdrop-filter: blur(8px);
  border-bottom: 1px solid var(--line);
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  margin-right: auto;
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

.tools {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-width: 0;
}

.account {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
  padding-left: 0.75rem;
  border-left: 1px solid var(--line);
}

/* Shown on phones too, where the username is hidden. */
.tier {
  flex: none;
  padding: 0.05rem 0.4rem;
  border-radius: 999px;
  background: var(--primary-soft);
  color: var(--primary);
  font-size: var(--fs-small);
  font-weight: 600;
  line-height: 1.4;
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
    flex-wrap: wrap;
    padding: 0.5rem 1rem 0.375rem;
    row-gap: 0.25rem;
    column-gap: 0.5rem;
  }

  .wordmark {
    font-size: 1.0625rem;
  }

  nav {
    order: 1;
    width: 100%;
    margin-left: -0.5rem;
  }

  nav a {
    padding: 0.25rem 0.5rem;
  }

  .tools {
    gap: 0.375rem;
  }

  .tier {
    padding: 0 0.3rem;
  }

  .account {
    padding-left: 0;
    border-left: none;
  }

  /* No room beside the brand; the account is still one tap away (logout). */
  .username {
    display: none;
  }
}
</style>
