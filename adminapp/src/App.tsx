import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { AuthProvider, useAuth } from './contexts/AuthContext'
import Dashboard from './pages/Dashboard'
import AdminUsers from './pages/AdminUsers'
import Login from './pages/Login'
import SystemHealthPage from './pages/SystemHealthPage'
import AIBehaviorPage from './pages/AIBehaviorPage'
import TenantsPage from './pages/TenantsPage'
import MessagingPage from './pages/MessagingPage'
import PostingStatusPage from './pages/PostingStatusPage'
import CostControlPage from './pages/CostControlPage'
import InsightsPage from './pages/InsightsPage'
import ScheduledPostsPage from './pages/ScheduledPostsPage'
import EscalationsPage from './pages/EscalationsPage'
import AnalyticsPage from './pages/AnalyticsPage'
import FailuresPage from './pages/FailuresPage'
import SocialAccountsPage from './pages/SocialAccountsPage'
import PaymentsPage from './pages/PaymentsPage'
import HealthPage from './pages/HealthPage'
import LogsPage from './pages/LogsPage'
import AuditLogsPage from './pages/AuditLogsPage'
import AlertsPage from './pages/AlertsPage'
import AlertTemplatesPage from './pages/AlertTemplatesPage'
import NewslettersPage from './pages/NewslettersPage'
import PlansPage from './pages/PlansPage'
import TenantDetailPage from './pages/TenantDetailPage'
import CheckpointsPage from './pages/CheckpointsPage'
import DashboardLayout from './components/layout/DashboardLayout'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
})

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth()

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div>Loading...</div>
      </div>
    )
  }

  if (!user) {
    return <Navigate to="/admin/login" replace />
  }

  return <>{children}</>
}

function AdminUsersPage() {
  const { user } = useAuth()
  if (!user || user.role !== 'SuperAdmin') {
    return (
      <DashboardLayout>
        <div className="text-center py-12">
          <h2 className="text-2xl font-bold text-gray-900 mb-2">Access Denied</h2>
          <p className="text-gray-600">You don't have permission to access this page.</p>
        </div>
      </DashboardLayout>
    )
  }
  return (
    <DashboardLayout>
      <AdminUsers />
    </DashboardLayout>
  )
}

function LayoutWrapper({ children }: { children: React.ReactNode }) {
  const { user } = useAuth()
  return (
    <DashboardLayout userRole={user?.role} userEmail={user?.email}>
      {children}
    </DashboardLayout>
  )
}

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/admin/login" element={<Login />} />
            <Route
              path="/admin/app"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <Dashboard />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/admin-users"
              element={
                <ProtectedRoute>
                  <AdminUsersPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/system-health"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <SystemHealthPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/ai-behavior"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <AIBehaviorPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/tenants"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <TenantsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/messaging"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <MessagingPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/posting"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <PostingStatusPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/cost"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <CostControlPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/insights"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <InsightsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/scheduled-posts"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <ScheduledPostsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/escalations"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <EscalationsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/analytics"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <AnalyticsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/failures"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <FailuresPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/social-accounts"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <SocialAccountsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/payments"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <PaymentsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/health"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <HealthPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/logs"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <LogsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/audit-logs"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <AuditLogsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/alerts"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <AlertsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/alert-templates"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <AlertTemplatesPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/newsletters"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <NewslettersPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/plans"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <PlansPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/tenants/:id"
              element={
                <ProtectedRoute>
                  <TenantDetailPage />
                </ProtectedRoute>
              }
            />
            <Route
              path="/admin/app/checkpoints"
              element={
                <ProtectedRoute>
                  <LayoutWrapper>
                    <CheckpointsPage />
                  </LayoutWrapper>
                </ProtectedRoute>
              }
            />
            <Route path="/" element={<Navigate to="/admin/app" replace />} />
          </Routes>
        </BrowserRouter>
      </AuthProvider>
    </QueryClientProvider>
  )
}

export default App

