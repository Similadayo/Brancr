import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Pagination } from '@/components/ui/pagination'
import { format } from 'date-fns'
import {
  Search,
  Eye,
  Users,
  AlertTriangle
} from 'lucide-react'
import { DataInterpretation } from '@/components/ui/data-interpretation'

interface TenantSummary {
  id: string
  name: string
  email: string
  plan_type: string
  status: string
  onboarding_step: string
  onboarding_complete: boolean
  platforms: string[]
  last_activity?: string
  usage_percent: number
  ai_human_mode: string
  kyb_status?: string
  admin_tags?: string[]
  pending_escalations: number
  created_at: string
}

interface TenantsListResponse {
  total: number
  items: TenantSummary[]
  limit: number
  offset: number
}

export default function TenantsPage() {
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [limit] = useState(50)
  const [offset, setOffset] = useState(0)

  const { data, isLoading } = useQuery<TenantsListResponse>({
    queryKey: ['tenants', statusFilter, search, limit, offset],
    queryFn: () => api.getTenants(statusFilter || undefined, search || undefined, limit, offset),
  })

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    setOffset(0) // Reset to first page on search
  }

  const getStatusBadge = (status: string) => {
    const variants: Record<string, { variant: 'default' | 'secondary' | 'destructive' | 'outline', label: string }> = {
      active: { variant: 'default', label: 'Active' },
      inactive: { variant: 'secondary', label: 'Inactive' },
      paused: { variant: 'outline', label: 'Paused' },
      suspended: { variant: 'destructive', label: 'Suspended' },
    }
    const config = variants[status] || { variant: 'secondary' as const, label: status }
    return <Badge variant={config.variant}>{config.label}</Badge>
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-gray-900">Tenants</h1>
        <p className="text-sm sm:text-base text-gray-600 mt-1">
          Manage all registered businesses ({data?.total || 0} total)
        </p>
      </div>

      {/* Data Interpretation */}
      <DataInterpretation
        title="Understanding Tenant Management"
        type="info"
      >
        <p>
          This page lists all businesses (tenants) registered on the Brancr platform. Here's what each column means:
        </p>
        <ul className="list-disc list-inside space-y-1.5 ml-2 mt-2">
          <li><strong>Business Name:</strong> The tenant's business name. The badge shows AI mode (AI/Human), pending escalations (🔔), and onboarding status.</li>
          <li><strong>Email:</strong> Primary contact email for the tenant account.</li>
          <li><strong>Plan:</strong> Current subscription tier (Basic, Pro, Enterprise, etc.)</li>
          <li><strong>Status:</strong> Account state - Active (normal), Inactive (dormant), Paused (temporarily disabled), or Suspended (violation).</li>
          <li><strong>Platforms:</strong> Social media platforms connected to their account (Instagram, Facebook, TikTok, etc.)</li>
          <li><strong>Created:</strong> Account registration date.</li>
          <li><strong>Pending Escalations:</strong> Number of customer requests awaiting human review. High numbers may indicate issues.</li>
        </ul>
        <p className="mt-2">
          <strong>Actions:</strong> Click "View" to access detailed tenant information, manage quotas, pause/unpause accounts, or review escalation history.
        </p>
      </DataInterpretation>

      {/* Filters */}
      <div className="flex flex-col sm:flex-row gap-4">
        <form onSubmit={handleSearch} className="flex-1">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-gray-400" />
            <Input
              type="text"
              placeholder="Search by name, email, or ID..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-10"
            />
          </div>
        </form>
        <div className="flex gap-2">
          <Button
            variant={statusFilter === '' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              setStatusFilter('')
              setOffset(0)
            }}
          >
            All
          </Button>
          <Button
            variant={statusFilter === 'active' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              setStatusFilter('active')
              setOffset(0)
            }}
          >
            Active
          </Button>
          <Button
            variant={statusFilter === 'inactive' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              setStatusFilter('inactive')
              setOffset(0)
            }}
          >
            Inactive
          </Button>
        </div>
      </div>

      {/* Table */}
      {isLoading ? (
        <div className="flex items-center justify-center py-12">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      ) : (
        <div className="bg-white rounded-lg border border-gray-200 shadow-sm overflow-hidden">
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    ID
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Business Name
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Email
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Plan
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Status
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Platforms
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Created
                  </th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody className="bg-white divide-y divide-gray-200">
                {data?.items?.length === 0 ? (
                  <tr>
                    <td colSpan={8} className="px-6 py-12 text-center text-sm text-gray-500">
                      No tenants found
                    </td>
                  </tr>
                ) : (
                  data?.items?.map((tenant) => (
                    <tr key={tenant.id} className="hover:bg-gray-50">
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">
                        {tenant.id}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="flex items-center">
                          <div>
                            <div className="text-sm font-medium text-gray-900">{tenant.name}</div>
                            <div className="flex items-center gap-2 mt-1">
                              {tenant.ai_human_mode === 'ai' && (
                                <Badge variant="outline" className="text-xs">
                                  <Users className="h-3 w-3 mr-1" />
                                  AI
                                </Badge>
                              )}
                              {tenant.pending_escalations > 0 && (
                                <Badge variant="destructive" className="text-xs">
                                  <AlertTriangle className="h-3 w-3 mr-1" />
                                  {tenant.pending_escalations}
                                </Badge>
                              )}
                              {!tenant.onboarding_complete && (
                                <Badge variant="secondary" className="text-xs">
                                  Incomplete
                                </Badge>
                              )}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                        {tenant.email}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <Badge variant="outline">{tenant.plan_type || 'N/A'}</Badge>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        {getStatusBadge(tenant.status)}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                        <div className="flex flex-wrap gap-1">
                          {tenant.platforms && tenant.platforms.length > 0 ? (
                            tenant.platforms.map((platform) => (
                              <Badge key={platform} variant="secondary" className="text-xs">
                                {platform}
                              </Badge>
                            ))
                          ) : (
                            <span className="text-gray-400">None</span>
                          )}
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                        {format(new Date(tenant.created_at), 'MMM d, yyyy')}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                        <Link to={`/admin/app/tenants/${tenant.id}`}>
                          <Button variant="outline" size="sm">
                            <Eye className="h-4 w-4 mr-1" />
                            View
                          </Button>
                        </Link>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
          {data && (
            <Pagination
              total={data.total}
              limit={limit}
              offset={offset}
              onPageChange={setOffset}
            />
          )}
        </div>
      )}
    </div>
  )
}
