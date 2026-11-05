import { createRouter, createWebHistory } from 'vue-router'

import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/AuthView.vue'), meta: { guest: true } },
    // 不加 guest：已登录用户点邮件里的 /forgot?token=xxx 必须能完成重置，
    // 否则会被守卫直接送到 /chat、把令牌丢掉
    { path: '/forgot', name: 'forgot', component: () => import('@/views/ForgotView.vue') },
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
    // 群邀请落地页：未登录时先跳登录，登录后回到这里
    {
      path: '/join/:token',
      name: 'join',
      component: () => import('@/views/JoinView.vue'),
      meta: { requiresAuth: true },
    },
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
  if (to.meta.guest && auth.token) return { name: 'chat' }
  if (to.meta.requiresAdmin) {
    // auth.user 只是内存态：刷新或直接访问 /admin 时它是空的，
    // 直接判 role 会把有权限的人挡回聊天页（表现为「后台进不去」）。
    // 有 token 就先补一次资料再判。
    if (!auth.user && auth.token) {
      try {
        await auth.fetchMe()
      } catch {
        /* 令牌失效由请求拦截器统一处理 */
      }
    }
    if (auth.user?.role !== 'admin') return { name: 'chat' }
  }
})

export default router
