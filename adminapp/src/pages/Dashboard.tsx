import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import StatCard from '@/components/dashboard/StatCard'
import SystemHealthSection from '@/components/dashboard/SystemHealthSection'
import AIBehaviorSection from '@/components/dashboard/AIBehaviorSection'
import TenantOverviewSection from '@/components/dashboard/TenantOverviewSection'
import MessagingActivitySection from '@/components/dashboard/MessagingActivitySection'
import PostingStatusSection from '@/components/dashboard/PostingStatusSection'
import CostControlSection from '@/components/dashboard/CostControlSection'
import QualitativeInsightsSection from '@/components/dashboard/QualitativeInsightsSection'
import {
  Activity,
  Users,
  MessageSquare,
  AlertCircle,
  DollarSign,
  Brain,
} from 'lucide-react'
import { DataInterpretation } from '@/components/ui/data-interpretation'
import { DateRangeFilter, DatePeriod } from '@/components/ui/date-range-filter'

// Role-based section visibility
function hasSectionAccess(role: string, section: string): boolean {
  if (role === 'SuperAdmin') return true

  const permissions: Record<string, string[]> = {
    'system_health': ['SuperAdmin', 'PlatformAdmin'],
    'ai_behavior': ['SuperAdmin', 'SupportAdmin'],
    'tenant_overview': ['SuperAdmin', 'PlatformAdmin', 'SupportAdmin', 'OperationsSupport'],
    'messaging_activity': ['SuperAdmin', 'SupportAdmin'],
    'posting_status': ['SuperAdmin', 'PlatformAdmin'],
    'cost_control': ['SuperAdmin'],
    'qualitative_insights': ['SuperAdmin', 'SupportAdmin', 'PlatformAdmin', 'OperationsSupport'],
  }

  const allowedRoles = permissions[section] || []
  return allowedRoles.includes(role)
}

export default function Dashboard() {
  const { user, loading: authLoading } = useAuth()
  const [period, setPeriod] = useState<DatePeriod>('24h')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')
  const currency: 'USD' | 'NGN' | 'EUR' | 'GBP' = 'USD' // Default to USD for dashboard overview

  // Fetch unified dashboard data
  const { data: dashboardData, isLoading } = useQuery({
    queryKey: ['dashboard', period, startDate, endDate],
    queryFn: () => api.getDashboard(period, startDate, endDate),
    enabled: !!user,
  })

  if (authLoading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600 mx-auto mb-4"></div>
          <p className="text-gray-600">Loading dashboard...</p>
        </div>
      </div>
    )
  }

  const role = user?.role || ''

  // Calculate stats from dashboard data
  const tenantCount = dashboardData?.tenant_overview?.summary?.total_tenants || 0
  const activeTenants = dashboardData?.tenant_overview?.summary?.active_tenants || 0
  const totalMessages = dashboardData?.messaging_activity?.total_conversations ||
    dashboardData?.messaging_activity?.total_messages || 0
  const totalCost = dashboardData?.cost_control?.total_cost ||
    dashboardData?.cost_control?.estimated_ai_cost || 0
  const aiCalls = dashboardData?.ai_behavior?.total_ai_calls ||
    dashboardData?.ai_behavior?.ai_replies_sent || 0
  const escalations = dashboardData?.ai_behavior?.escalations_triggered || 0

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-gray-900">Dashboard</h1>
          <p className="text-sm sm:text-base text-gray-600 mt-1">Welcome back, {user?.email}</p>
        </div>
        <div className="flex gap-2">
          <DateRangeFilter
            period={period}
            setPeriod={setPeriod}
            startDate={startDate}
            setStartDate={setStartDate}
            endDate={endDate}
            setEndDate={setEndDate}
          />
        </div>
      </div>

      {/* Data Interpretation */}
      <DataInterpretation
        title="Dashboard Overview Guide"
        type="info"
      >
        <p>
          This dashboard provides a real-time overview of Brancr's platform activity. Here's what each section means:
        </p>
        <ul className="list-disc list-inside space-y-1.5 ml-2 mt-2">
          <li><strong>Total Tenants:</strong> All registered business accounts on the platform</li>
          <li><strong>Active Tenants:</strong> Tenants who have used AI features or messaging in the selected period</li>
          <li><strong>Messages:</strong> Total customer interactions processed (WhatsApp, Instagram, etc.)</li>
          <li><strong>AI Calls:</strong> Number of AI operations performed (conversations, caption generation, etc.)</li>
          <li><strong>Estimated Cost:</strong> Total spending on AI services (OpenAI API) during this period</li>
          <li><strong>Escalations:</strong> Customer requests that required human intervention</li>
        </ul>
        <p className="mt-2">
          <strong>Quick Actions:</strong> Click on any section to view detailed metrics. Use the time period selector to compare different timeframes.
        </p>
      </DataInterpretation>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          title="Total Tenants"
          value={tenantCount}
          icon={Users}
          iconColor="text-blue-500"
          gradient="from-blue-500/10 to-blue-600/10"
        />
        <StatCard
          title="Active Tenants"
          value={activeTenants}
          icon={Activity}
          iconColor="text-green-500"
          gradient="from-green-500/10 to-green-600/10"
        />
        <StatCard
          title="Messages"
          value={totalMessages}
          icon={MessageSquare}
          iconColor="text-purple-500"
          gradient="from-purple-500/10 to-purple-600/10"
        />
        <StatCard
          title="AI Calls"
          value={aiCalls}
          icon={Brain}
          iconColor="text-orange-500"
          gradient="from-orange-500/10 to-orange-600/10"
        />
        {hasSectionAccess(role, 'cost_control') && (
          <StatCard
            title="Estimated Cost"
            value={`$${totalCost.toFixed(2)}`}
            icon={DollarSign}
            iconColor="text-emerald-500"
            gradient="from-emerald-500/10 to-emerald-600/10"
          />
        )}
        <StatCard
          title="Escalations"
          value={escalations}
          icon={AlertCircle}
          iconColor="text-red-500"
          gradient="from-red-500/10 to-red-600/10"
        />
      </div>

      {/* Main Content Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Left Column */}
        <div className="space-y-6">
          {/* System Health */}
          {hasSectionAccess(role, 'system_health') && (
            <SystemHealthSection
              period={period}
              startDate={startDate}
              endDate={endDate}
            />
          )}

          {/* Messaging Activity */}
          {hasSectionAccess(role, 'messaging_activity') && (
            <MessagingActivitySection period={period} startDate={startDate} endDate={endDate} />
          )}

          {/* Posting Status */}
          {hasSectionAccess(role, 'posting_status') && (
            <PostingStatusSection period={period} startDate={startDate} endDate={endDate} />
          )}
        </div>

        {/* Right Column */}
        <div className="space-y-6">
          {/* AI Behavior */}
          {hasSectionAccess(role, 'ai_behavior') && (
            <AIBehaviorSection period={period} startDate={startDate} endDate={endDate} />
          )}

          {/* Cost Control */}
          {hasSectionAccess(role, 'cost_control') && (
            <CostControlSection
              period={period}
              currency={currency}
              startDate={startDate && period === 'custom' ? startDate : undefined}
              endDate={endDate && period === 'custom' ? endDate : undefined}
            />
          )}

          {/* Qualitative Insights */}
          {hasSectionAccess(role, 'qualitative_insights') && (
            <QualitativeInsightsSection period={period} />
          )}
        </div>
      </div>

      {/* Full Width Sections */}
      {hasSectionAccess(role, 'tenant_overview') && (
        <TenantOverviewSection />
      )}

      {isLoading && (
        <div className="fixed inset-0 bg-black/20 backdrop-blur-sm z-50 flex items-center justify-center">
          <div className="bg-white rounded-xl p-6 shadow-xl">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto mb-2"></div>
            <p className="text-sm text-gray-600">Loading dashboard data...</p>
          </div>
        </div>
      )}
    </div>
  )
}
