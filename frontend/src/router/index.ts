import { createRouter, createWebHistory } from 'vue-router'
import HomeView from '../views/HomeView.vue'
import { useAuthStore } from '@/stores/auth'

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
      path: '/connections/:clustername?',
      name: 'connections',
      component: () => import('../views/ConnectionsView.vue'),
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
      path: '/query-report/:clustername?',
      name: 'query-report',
      component: () => import('../views/QueryReportView.vue'),
    },
    {
      path: '/tables/:clustername?',
      name: 'tables',
      component: () => import('../views/TablesView.vue'),
    },
    {
      path: '/table-describe/:clustername?',
      name: 'table-describe',
      component: () => import('../views/TableDescribeView.vue'),
    },
    {
      path: '/indexes/:clustername?',
      name: 'indexes',
      component: () => import('../views/IndexesOverviewView.vue'),
    },
    {
      path: '/indexes-usage/:clustername?',
      name: 'indexes-usage',
      component: () => import('../views/IndexesUsageView.vue'),
    },
    {
      path: '/indexes-problems/:clustername?',
      name: 'indexes-problems',
      component: () => import('../views/IndexesProblemsView.vue'),
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
      path: '/replication/:clustername?',
      name: 'replication',
      component: () => import('../views/ReplicationView.vue'),
    },
    {
      path: '/settings/:clustername?',
      name: 'settings',
      component: () => import('../views/SettingsView.vue'),
    },
  ],
})

router.beforeEach(async () => {
  const auth = useAuthStore()

  if (!auth.initialized) {
    await auth.init()
  }

  if (auth.redirecting) {
    auth.doLoginRedirect()
    return false
  }
})

export default router
