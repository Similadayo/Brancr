import { useState } from 'react'
import { useQuery, keepPreviousData } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Users, Clock, CheckCircle2, ChevronLeft, ChevronRight } from 'lucide-react'

import type { TenantOverviewData } from '@/types/dashboard'

export default function TenantOverviewSection() {
  const [page, setPage] = useState(1)
  const limit = 10
  const offset = (page - 1) * limit

  const { data, isLoading } = useQuery<TenantOverviewData>({
    queryKey: ['tenant-overview', limit, offset],
    queryFn: () => api.getTenantOverview(limit, offset),
    placeholderData: keepPreviousData,
  })

  if (isLoading && !data) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Tenant & Onboarding Overview</CardTitle>
          <CardDescription>Loading...</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (!data) {
    return (
      <Card className="shadow-sm border-gray-200">
        <CardHeader>
          <CardTitle className="text-xl font-semibold">Tenant Overview</CardTitle>
          <CardDescription>No data available</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  const summary = data.summary || {}
  const totalPages = Math.ceil((summary.total_tenants || 0) / limit)

  return (
    <Card className="shadow-sm border-gray-200">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-xl font-semibold">Tenant Overview</CardTitle>
            <CardDescription className="mt-1">Tenant status and onboarding progress</CardDescription>
          </div>
          <div className="h-10 w-10 rounded-lg bg-blue-100 flex items-center justify-center">
            <Users className="h-5 w-5 text-blue-600" />
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="grid grid-cols-2 md:grid-cols-5 gap-4 mb-6">
          <div className="bg-gradient-to-br from-blue-50/50 to-transparent rounded-lg p-4 border border-blue-100">
            <div className="flex items-center gap-2 mb-2">
              <Users className="h-4 w-4 text-blue-500" />
              <p className="text-xs font-medium text-gray-600">Total Tenants</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">{summary.total_tenants || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-green-50/50 to-transparent rounded-lg p-4 border border-green-100">
            <div className="flex items-center gap-2 mb-2">
              <CheckCircle2 className="h-4 w-4 text-green-500" />
              <p className="text-xs font-medium text-gray-600">Active</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">{summary.active_tenants || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-emerald-50/50 to-transparent rounded-lg p-4 border border-emerald-100">
            <p className="text-xs font-medium text-gray-600 mb-2">Onboarding Complete</p>
            <p className="text-3xl font-bold text-gray-900">{summary.onboarding_complete || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-purple-50/50 to-transparent rounded-lg p-4 border border-purple-100">
            <p className="text-xs font-medium text-gray-600 mb-2">AI Enabled</p>
            <p className="text-3xl font-bold text-gray-900">{summary.ai_enabled_count || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-orange-50/50 to-transparent rounded-lg p-4 border border-orange-100">
            <p className="text-xs font-medium text-gray-600 mb-2">Needs Followup</p>
            <p className="text-3xl font-bold text-gray-900">{summary.needs_followup || 0}</p>
          </div>
        </div>

        {data.tenants && data.tenants.length > 0 ? (
          <div className="border-t border-gray-200 pt-4">
            <h4 className="font-semibold text-base mb-4 text-gray-900">Recent Tenants</h4>
            <div className="space-y-2 mb-4">
              {data.tenants.map((tenant: any) => (
                <Link
                  key={tenant.id}
                  to={`/admin/app/tenants/${tenant.id}`}
                  className="flex items-center justify-between p-4 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors bg-white cursor-pointer"
                >
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      <span className="font-medium hover:underline">{tenant.name}</span>
                      {tenant.ai_enabled ? (
                        <Badge variant="success" className="text-xs">AI Enabled</Badge>
                      ) : (
                        <Badge variant="secondary" className="text-xs">AI Disabled</Badge>
                      )}
                      {tenant.onboarding_status === 'activated' && (
                        <Badge variant="default" className="text-xs">Activated</Badge>
                      )}
                    </div>
                    <div className="flex items-center gap-4 text-xs text-muted-foreground">
                      <span>{tenant.industry || 'N/A'}</span>
                      {tenant.platforms_connected && tenant.platforms_connected.length > 0 && (
                        <span>{tenant.platforms_connected.length} platforms</span>
                      )}
                      {tenant.time_to_first_value && (
                        <span className="flex items-center gap-1">
                          <Clock className="h-3 w-3" />
                          {tenant.time_to_first_value}
                        </span>
                      )}
                    </div>
                  </div>
                </Link>
              ))}
            </div>

            {/* Pagination Controls */}
            {totalPages > 1 && (
              <div className="flex items-center justify-between border-t border-gray-200 pt-4">
                <p className="text-sm text-gray-600">
                  Showing <span className="font-medium">{(page - 1) * limit + 1}</span> to{' '}
                  <span className="font-medium">
                    {Math.min(page * limit, summary.total_tenants || 0)}
                  </span>{' '}
                  of <span className="font-medium">{summary.total_tenants}</span> tenants
                </p>
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage((p) => Math.max(1, p - 1))}
                    disabled={page === 1 || isLoading}
                  >
                    <ChevronLeft className="h-4 w-4" />
                    Previous
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                    disabled={page >= totalPages || isLoading}
                  >
                    Next
                    <ChevronRight className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            )}
          </div>
        ) : (
          <p className="text-muted-foreground">No tenant data available</p>
        )}
      </CardContent>
    </Card>
  )
}

