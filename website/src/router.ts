import { createRouter, createWebHistory } from 'vue-router'

import DocsView from '@/views/DocsView.vue'
import HomeView from '@/views/HomeView.vue'
import PluginsView from '@/views/PluginsView.vue'

// history 模式：页内锚点（#features 等）才能正常工作。
// 部署时需在静态服务器上加 fallback：nginx → try_files $uri $uri/ /index.html
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'home', component: HomeView },
    { path: '/docs/:file?', name: 'docs', component: DocsView },
    { path: '/plugins', name: 'plugins', component: PluginsView },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
  scrollBehavior(to, _from, saved) {
    if (to.hash) return { el: to.hash, behavior: 'smooth', top: 72 }
    if (saved) return saved
    return { top: 0 }
  },
})
