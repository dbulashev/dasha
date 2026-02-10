import { createRouter, createWebHistory } from 'vue-router'
import HomeView from '../views/HomeView.vue'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'main',
      redirect: '/main/',
    },
    {
      path: '/main/:clustername?',
      name: 'home',
      component: HomeView,
    },
    {
      path: '/queries/:clustername?',
      name: 'queries',
      component: () => import('../views/QueriesView.vue'),
    },
    {
      path: '/query-stats/:clustername?',
      name: 'query-stats',
      component: () => import('../views/QueryStatsView.vue'),
    },
    {
      path: '/tables/:clustername?',
      name: 'tables',
      component: () => import('../views/TablesView.vue'),
    },
    {
      path: '/indexes/:clustername?',
      name: 'indexes',
      component: () => import('../views/IndexesView.vue'),
    },
    {
      path: '/locks/:clustername?',
      name: 'locks',
      component: () => import('../views/LocksView.vue'),
    },
    {
      path: '/progress/:clustername?',
      name: 'progress',
      component: () => import('../views/ProgressView.vue'),
    },
    {
      path: '/fk-analysis/:clustername?',
      name: 'fk-analysis',
      component: () => import('../views/FkAnalysisView.vue'),
    },
    {
      path: '/maintenance/:clustername?',
      name: 'maintenance',
      component: () => import('../views/MaintenanceView.vue'),
    },
    {
      path: '/settings/:clustername?',
      name: 'settings',
      component: () => import('../views/SettingsView.vue'),
    },
  ],
})

export default router
