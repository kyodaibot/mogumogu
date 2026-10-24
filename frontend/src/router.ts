import { createRouter, createWebHistory } from 'vue-router'
import { onUnauthorized } from './api'
import { session } from './session'
import CalendarPage from './pages/CalendarPage.vue'
import LoginPage from './pages/LoginPage.vue'
import ProfilePage from './pages/ProfilePage.vue'

declare module 'vue-router' {
  interface RouteMeta {
    /** Reachable without logging in. */
    public?: boolean
  }
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: CalendarPage },
    { path: '/profile', component: ProfilePage },
    { path: '/login', component: LoginPage, meta: { public: true } },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

router.beforeEach(async (to) => {
  if (!session.state.loaded) {
    // A backend that is down is handled like being logged out; the login page
    // then shows the connection error when the user tries to log in.
    await session.load().catch(() => {})
  }
  const { user, hasProfile } = session.state

  if (to.meta.public) {
    return user ? (hasProfile ? '/' : '/profile') : true
  }
  if (!user) {
    return { path: '/login', query: to.fullPath === '/' ? {} : { redirect: to.fullPath } }
  }
  // First-time users fill in their profile before using the calendar.
  if (!hasProfile && to.path !== '/profile') {
    return '/profile'
  }
  return true
})

onUnauthorized(() => {
  session.clear()
  if (!router.currentRoute.value.meta.public) {
    void router.push('/login')
  }
})
