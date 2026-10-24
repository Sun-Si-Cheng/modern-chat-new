import { createRouter, createWebHistory } from 'vue-router'

import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/AuthView.vue'), meta: { guest: true } },
    { path: '/forgot', name: 'forgot', component: () => import('@/views/ForgotView.vue'), meta: { guest: true } },
    { path: '/chat', name: 'chat', component: () => import('@/views/ChatLayout.vue'), meta: { requiresAuth: true } },
    {
      path: '/admin',
      name: 'admin',
      component: () => import('@/views/admin/AdminView.vue'),
      meta: { requiresAuth: true, requiresAdmin: true },
    },
    { path: '/', redirect: '/chat' },
    { path: '/:pathMatch(.*)*', redirect: '/chat' },
  ],
})

router.beforeEach((to) => {
  const auth = useAuthStore()
  if (to.meta.requiresAuth && !auth.token) {
    return { name: 'login', query: to.fullPath === '/chat' ? {} : { redirect: to.fullPath } }
  }
  if (to.meta.guest && auth.token) return { name: 'chat' }
  if (to.meta.requiresAdmin && auth.user?.role !== 'admin') return { name: 'chat' }
})

export default router
