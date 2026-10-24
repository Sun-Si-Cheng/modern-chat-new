import { createRouter, createWebHistory } from 'vue-router'

import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/AuthView.vue'), meta: { guest: true } },
    { path: '/forgot', name: 'forgot', component: () => import('@/views/ForgotView.vue'), meta: { guest: true } },
    // 设置：公开（未登录时仅可改外观）
    {
      path: '/settings',
      name: 'settings',
      component: () => import('@/views/SettingsView.vue'),
    },
    // 法律文档：公开可访问（登录前后都能打开）
    {
      path: '/terms',
      name: 'terms',
      component: () => import('@/views/LegalView.vue'),
      meta: { legal: 'terms' },
    },
    {
      path: '/privacy',
      name: 'privacy',
      component: () => import('@/views/LegalView.vue'),
      meta: { legal: 'privacy' },
    },
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

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (to.meta.requiresAuth && !auth.token) {
    return { name: 'login', query: to.fullPath === '/chat' ? {} : { redirect: to.fullPath } }
  }
  // 刷新页面时 token 从 localStorage 恢复、但 user 还未拉取：先补拉再判权限，
  // 否则刷新 /admin 会因 user 为 null 被误判成非站长而踢回 /chat
  if (to.meta.requiresAuth && auth.token && !auth.user) {
    await auth.fetchMe()
    // token 失效时 fetchMe 内部已 logout
    if (!auth.token || !auth.user) {
      return { name: 'login', query: to.fullPath === '/chat' ? {} : { redirect: to.fullPath } }
    }
  }
  if (to.meta.guest && auth.token) return { name: 'chat' }
  if (to.meta.requiresAdmin && auth.user?.role !== 'admin') return { name: 'chat' }
})

export default router
