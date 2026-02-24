import { useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import {
  LayoutDashboard,
  Users,
  Activity,
  Brain,
  DollarSign,
  MessageSquare,
  FileText,
  Settings,
  LogOut,
  Menu,
  X,
  Calendar,
  AlertTriangle,
  BarChart3,
  Share2,
  CreditCard,
  Heart,
  BookOpen,
  Mail,
  Package,
  Flag
} from 'lucide-react'

interface DashboardLayoutProps {
  children: React.ReactNode
  userRole?: string
  userEmail?: string
}

export default function DashboardLayout({ children, userRole, userEmail }: DashboardLayoutProps) {
  // Default to SuperAdmin if not provided (for backwards compatibility)
  const effectiveRole = userRole || 'SuperAdmin'
  const effectiveEmail = userEmail || ''
  const location = useLocation()
  const [sidebarOpen, setSidebarOpen] = useState(false)

  const navItems = [
    {
      icon: LayoutDashboard,
      label: 'Dashboard',
      path: '/admin/app',
      roles: ['SuperAdmin', 'SupportAdmin', 'PlatformAdmin', 'OperationsSupport', 'BillingAdmin', 'ComplianceAdmin']
    },
    {
      icon: Activity,
      label: 'System Health',
      path: '/admin/app/system-health',
      roles: ['SuperAdmin', 'PlatformAdmin']
    },
    {
      icon: Brain,
      label: 'AI Behavior',
      path: '/admin/app/ai-behavior',
      roles: ['SuperAdmin', 'SupportAdmin']
    },
    {
      icon: Users,
      label: 'Tenants',
      path: '/admin/app/tenants',
      roles: ['SuperAdmin', 'PlatformAdmin', 'SupportAdmin', 'OperationsSupport']
    },
    {
      icon: MessageSquare,
      label: 'Messaging',
      path: '/admin/app/messaging',
      roles: ['SuperAdmin', 'SupportAdmin']
    },
    {
      icon: FileText,
      label: 'Posting Status',
      path: '/admin/app/posting',
      roles: ['SuperAdmin', 'PlatformAdmin']
    },
    {
      icon: DollarSign,
      label: 'Cost Control',
      path: '/admin/app/cost',
      roles: ['SuperAdmin']
    },
    {
      icon: FileText,
      label: 'Insights',
      path: '/admin/app/insights',
      roles: ['SuperAdmin', 'SupportAdmin', 'PlatformAdmin', 'OperationsSupport']
    },
    {
      icon: Calendar,
      label: 'Scheduled Posts',
      path: '/admin/app/scheduled-posts',
      roles: ['SuperAdmin', 'PlatformAdmin', 'OperationsSupport']
    },
    {
      icon: AlertTriangle,
      label: 'Escalations',
      path: '/admin/app/escalations',
      roles: ['SuperAdmin', 'SupportAdmin', 'PlatformAdmin']
    },
    {
      icon: BarChart3,
      label: 'Analytics',
      path: '/admin/app/analytics',
      roles: ['SuperAdmin', 'PlatformAdmin', 'OperationsSupport']
    },
    {
      icon: Heart,
      label: 'Failures',
      path: '/admin/app/failures',
      roles: ['SuperAdmin', 'SupportAdmin', 'PlatformAdmin', 'OperationsSupport']
    },
    {
      icon: Share2,
      label: 'Social Accounts',
      path: '/admin/app/social-accounts',
      roles: ['SuperAdmin', 'PlatformAdmin', 'OperationsSupport']
    },
    {
      icon: CreditCard,
      label: 'Payments',
      path: '/admin/app/payments',
      roles: ['SuperAdmin', 'BillingAdmin']
    },
    {
      icon: Package,
      label: 'Plans',
      path: '/admin/app/plans',
      roles: ['SuperAdmin', 'BillingAdmin']
    },
    {
      icon: Activity,
      label: 'Health',
      path: '/admin/app/health',
      roles: ['SuperAdmin', 'OperationsSupport']
    },
    {
      icon: BookOpen,
      label: 'System Logs',
      path: '/admin/app/logs',
      roles: ['SuperAdmin', 'OperationsSupport']
    },
    {
      icon: BookOpen,
      label: 'Audit Logs',
      path: '/admin/app/audit-logs',
      roles: ['SuperAdmin', 'ComplianceAdmin']
    },
    {
      icon: AlertTriangle,
      label: 'Alerts',
      path: '/admin/app/alerts',
      roles: ['SuperAdmin', 'SupportAdmin', 'PlatformAdmin']
    },
    {
      icon: MessageSquare,
      label: 'Alert Templates',
      path: '/admin/app/alert-templates',
      roles: ['SuperAdmin', 'PlatformAdmin']
    },
    {
      icon: Flag,
      label: 'Experience Checkpoints',
      path: '/admin/app/checkpoints',
      roles: ['SuperAdmin', 'SupportAdmin', 'PlatformAdmin']
    },
    {
      icon: Mail,
      label: 'Newsletters',
      path: '/admin/app/newsletters',
      roles: ['SuperAdmin', 'PlatformAdmin']
    },
  ]

  const adminOnlyItems = [
    {
      icon: Settings,
      label: 'Admin Users',
      path: '/admin/app/admin-users',
      roles: ['SuperAdmin']
    },
  ]

  const filteredNavItems = navItems.filter(item =>
    item.roles.includes(effectiveRole) || effectiveRole === 'SuperAdmin'
  )

  const filteredAdminItems = adminOnlyItems.filter(item =>
    item.roles.includes(effectiveRole) || effectiveRole === 'SuperAdmin'
  )

  const handleLogout = () => {
    // TODO: Implement logout
    window.location.href = '/admin/login'
  }

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Mobile sidebar toggle */}
      <div className="lg:hidden fixed top-0 left-0 right-0 z-50 bg-white border-b border-gray-200 px-4 py-3 flex items-center justify-between shadow-sm">
        <h1 className="text-lg font-semibold text-gray-900">Brancr</h1>
        <button
          onClick={() => setSidebarOpen(!sidebarOpen)}
          className="p-2 rounded-md text-gray-600 hover:bg-gray-100 transition-colors"
        >
          {sidebarOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
        </button>
      </div>

      {/* Sidebar - Fixed on all screen sizes */}
      <aside
        className={`fixed top-0 left-0 h-screen w-64 bg-gray-50 border-r border-gray-200 transform transition-transform duration-200 ease-in-out z-40 ${sidebarOpen ? 'translate-x-0' : '-translate-x-full'
          } lg:translate-x-0`}
      >
        <div className="h-full flex flex-col overflow-hidden">
          {/* Logo/Brand - Minimal */}
          <div className="p-4 border-b border-gray-200 flex-shrink-0">
            <h1 className="text-xl font-semibold text-gray-900">Brancr</h1>
          </div>

          {/* Navigation - Grouped by sections - Scrollable if content overflows */}
          <nav className="flex-1 overflow-y-auto overflow-x-hidden p-3 space-y-6">
            {/* Core Operations */}
            <div>
              <p className="px-3 text-[10px] font-semibold text-gray-400 uppercase tracking-wider mb-2">
                Core
              </p>
              <div className="space-y-0.5">
                {filteredNavItems.slice(0, 6).map((item) => {
                  const Icon = item.icon
                  const isActive = location.pathname === item.path ||
                    (item.path !== '/admin/app' && location.pathname.startsWith(item.path))

                  return (
                    <Link
                      key={item.path}
                      to={item.path}
                      onClick={() => setSidebarOpen(false)}
                      className={`flex items-center gap-2.5 px-3 py-2 rounded-md text-sm transition-colors ${isActive
                        ? 'bg-green-50 text-green-700 font-medium'
                        : 'text-gray-600 hover:bg-gray-100'
                        }`}
                    >
                      <Icon className="h-4 w-4 flex-shrink-0" />
                      <span className="truncate">{item.label}</span>
                    </Link>
                  )
                })}
              </div>
            </div>

            {/* Analytics & Insights */}
            <div>
              <p className="px-3 text-[10px] font-semibold text-gray-400 uppercase tracking-wider mb-2">
                Analytics
              </p>
              <div className="space-y-0.5">
                {filteredNavItems.slice(6, 11).map((item) => {
                  const Icon = item.icon
                  const isActive = location.pathname === item.path ||
                    (item.path !== '/admin/app' && location.pathname.startsWith(item.path))

                  return (
                    <Link
                      key={item.path}
                      to={item.path}
                      onClick={() => setSidebarOpen(false)}
                      className={`flex items-center gap-2.5 px-3 py-2 rounded-md text-sm transition-colors ${isActive
                        ? 'bg-green-50 text-green-700 font-medium'
                        : 'text-gray-600 hover:bg-gray-100'
                        }`}
                    >
                      <Icon className="h-4 w-4 flex-shrink-0" />
                      <span className="truncate">{item.label}</span>
                    </Link>
                  )
                })}
              </div>
            </div>

            {/* Management */}
            <div>
              <p className="px-3 text-[10px] font-semibold text-gray-400 uppercase tracking-wider mb-2">
                Management
              </p>
              <div className="space-y-0.5">
                {filteredNavItems.slice(11).map((item) => {
                  const Icon = item.icon
                  const isActive = location.pathname === item.path ||
                    (item.path !== '/admin/app' && location.pathname.startsWith(item.path))

                  return (
                    <Link
                      key={item.path}
                      to={item.path}
                      onClick={() => setSidebarOpen(false)}
                      className={`flex items-center gap-2.5 px-3 py-2 rounded-md text-sm transition-colors ${isActive
                        ? 'bg-green-50 text-green-700 font-medium'
                        : 'text-gray-600 hover:bg-gray-100'
                        }`}
                    >
                      <Icon className="h-4 w-4 flex-shrink-0" />
                      <span className="truncate">{item.label}</span>
                    </Link>
                  )
                })}
              </div>
            </div>

            {/* Administration Section */}
            {filteredAdminItems.length > 0 && (
              <div>
                <p className="px-3 text-[10px] font-semibold text-gray-400 uppercase tracking-wider mb-2">
                  System
                </p>
                <div className="space-y-0.5">
                  {filteredAdminItems.map((item) => {
                    const Icon = item.icon
                    const isActive = location.pathname === item.path

                    return (
                      <Link
                        key={item.path}
                        to={item.path}
                        onClick={() => setSidebarOpen(false)}
                        className={`flex items-center gap-2.5 px-3 py-2 rounded-md text-sm transition-colors ${isActive
                          ? 'bg-green-50 text-green-700 font-medium'
                          : 'text-gray-600 hover:bg-gray-100'
                          }`}
                      >
                        <Icon className="h-4 w-4 flex-shrink-0" />
                        <span className="truncate">{item.label}</span>
                      </Link>
                    )
                  })}
                </div>
              </div>
            )}
          </nav>

          {/* User info and logout - Fixed at bottom */}
          <div className="p-3 border-t border-gray-200 flex-shrink-0">
            <div className="mb-3 flex items-center gap-2.5 px-2">
              <div className="h-8 w-8 rounded-full bg-gray-300 flex items-center justify-center flex-shrink-0">
                <span className="text-xs font-medium text-gray-600">
                  {effectiveEmail.charAt(0).toUpperCase()}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-xs font-medium text-gray-900 truncate">{effectiveEmail}</p>
                <p className="text-[10px] text-gray-500">{effectiveRole}</p>
              </div>
            </div>
            <button
              onClick={handleLogout}
              className="w-full flex items-center gap-2.5 px-2 py-2 text-sm text-gray-600 hover:bg-gray-100 rounded-md transition-colors"
            >
              <LogOut className="h-4 w-4 flex-shrink-0" />
              <span>Log out</span>
            </button>
          </div>
        </div>
      </aside>

      {/* Overlay for mobile */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 bg-black bg-opacity-50 z-30 lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Main content - Independent scrolling */}
      <main className={`fixed top-0 right-0 transition-all duration-200 ${sidebarOpen ? 'pt-16' : 'pt-0'} lg:pt-0 lg:left-64 left-0 bottom-0 overflow-y-auto bg-white`}>
        <div className="p-4 sm:p-6 lg:p-8">
          {children}
        </div>
      </main>
    </div>
  )
}

