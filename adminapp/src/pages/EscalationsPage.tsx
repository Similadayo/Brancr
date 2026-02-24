import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { format } from 'date-fns'
import { AlertCircle, CheckCircle2, TrendingUp, TrendingDown } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useState } from 'react'
import { DateRangeFilter, DatePeriod } from '@/components/ui/date-range-filter'
import { DataInterpretation } from '@/components/ui/data-interpretation'

interface TenantEscalationMetrics {
  tenant_id: string
  tenant_name: string
  total_escalations: number
  pending_count: number
  resolved_count: number
  failed_count: number
  success_rate: number
  last_escalation_at?: string
}

interface EscalationMetricsResponse {
  total_tenants: number
  metrics: TenantEscalationMetrics[]
}

export default function EscalationsPage() {
  const [period, setPeriod] = useState<DatePeriod>('30d')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')

  const { data, isLoading } = useQuery<EscalationMetricsResponse>({
    queryKey: ['escalations-metrics', period, startDate, endDate],
    queryFn: () => api.getEscalationMetrics(period, startDate, endDate),
  })

  if (isLoading) {
    return <div className="p-6">Loading escalation metrics...</div>
  }

  // Calculate totals
  const totals = data?.metrics.reduce(
    (acc, m) => ({
      total: acc.total + m.total_escalations,
      pending: acc.pending + m.pending_count,
      resolved: acc.resolved + m.resolved_count,
      failed: acc.failed + m.failed_count,
    }),
    { total: 0, pending: 0, resolved: 0, failed: 0 }
  ) || { total: 0, pending: 0, resolved: 0, failed: 0 }

  const overallSuccessRate =
    totals.total > 0 ? ((totals.resolved / totals.total) * 100).toFixed(1) : '0.0'

  // Sort by total escalations (descending)
  const sortedMetrics = [...(data?.metrics || [])].sort(
    (a, b) => b.total_escalations - a.total_escalations
  )

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-gray-900">Escalations</h1>
          <p className="text-sm sm:text-base text-gray-600 mt-1">
            Escalation metrics and success rates per tenant
          </p>
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
        title="Understanding Escalation Metrics"
        type="info"
      >
        <p>
          Escalations occur when customer requests need human intervention - either because the AI couldn't handle them or they require special attention. This page shows how well we're managing these escalations.
        </p>
        <ul className="list-disc list-inside space-y-1.5 ml-2 mt-2">
          <li><strong>Total Escalations:</strong> All customer requests that required human review during the selected period.</li>
          <li><strong>Pending:</strong> Escalations currently awaiting staff response. High numbers indicate a backlog that needs attention.</li>
          <li><strong>Resolved:</strong> Escalations successfully handled by support staff. This is your primary success metric.</li>
          <li><strong>Failed:</strong> Escalations that couldn't be resolved (usually due to technical issues or communication problems). Should be minimal.</li>
          <li><strong>Success Rate:</strong> Percentage of escalations successfully resolved. Target: 80%+ for good service quality.</li>
        </ul>
        <p className="mt-2">
          <strong>Action Items:</strong> Monitor tenants with low success rates (&lt;50%) or high pending counts. These may need additional support resources or training. Click "View Details" on any tenant to see specific escalation cases.
        </p>
      </DataInterpretation>

      {/* Summary Cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Escalations</CardTitle>
            <AlertCircle className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totals.total}</div>
            <p className="text-xs text-muted-foreground">Across {data?.total_tenants || 0} tenants</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Pending</CardTitle>
            <AlertCircle className="h-4 w-4 text-yellow-600" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-yellow-600">{totals.pending}</div>
            <p className="text-xs text-muted-foreground">
              {totals.total > 0 ? ((totals.pending / totals.total) * 100).toFixed(1) : '0.0'}% of total
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Resolved</CardTitle>
            <CheckCircle2 className="h-4 w-4 text-green-600" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-600">{totals.resolved}</div>
            <p className="text-xs text-muted-foreground">
              {totals.total > 0 ? ((totals.resolved / totals.total) * 100).toFixed(1) : '0.0'}% of total
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Success Rate</CardTitle>
            {parseFloat(overallSuccessRate) >= 80 ? (
              <TrendingUp className="h-4 w-4 text-green-600" />
            ) : (
              <TrendingDown className="h-4 w-4 text-red-600" />
            )}
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{overallSuccessRate}%</div>
            <p className="text-xs text-muted-foreground">Resolved vs total</p>
          </CardContent>
        </Card>
      </div>

      {/* Tenant Metrics Table */}
      <Card>
        <CardHeader>
          <CardTitle>Escalation Metrics by Tenant</CardTitle>
          <CardDescription>Breakdown of escalations, success rates, and status per tenant</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b">
                  <th className="text-left p-2">Tenant</th>
                  <th className="text-right p-2">Total</th>
                  <th className="text-right p-2">Pending</th>
                  <th className="text-right p-2">Resolved</th>
                  <th className="text-right p-2">Failed</th>
                  <th className="text-right p-2">Success Rate</th>
                  <th className="text-right p-2">Last Escalation</th>
                  <th className="text-center p-2">Actions</th>
                </tr>
              </thead>
              <tbody>
                {sortedMetrics.map((metric) => (
                  <tr key={metric.tenant_id} className="border-b hover:bg-muted/50">
                    <td className="p-2">
                      <Link
                        to={`/admin/app/tenants/${metric.tenant_id}`}
                        className="font-medium hover:underline"
                      >
                        {metric.tenant_name}
                      </Link>
                      <div className="text-xs text-muted-foreground">ID: {metric.tenant_id}</div>
                    </td>
                    <td className="text-right p-2 font-medium">{metric.total_escalations}</td>
                    <td className="text-right p-2">
                      {metric.pending_count > 0 ? (
                        <Badge variant="secondary">{metric.pending_count}</Badge>
                      ) : (
                        <span className="text-muted-foreground">0</span>
                      )}
                    </td>
                    <td className="text-right p-2">
                      <span className="text-green-600 font-medium">{metric.resolved_count}</span>
                    </td>
                    <td className="text-right p-2">
                      {metric.failed_count > 0 ? (
                        <Badge variant="destructive">{metric.failed_count}</Badge>
                      ) : (
                        <span className="text-muted-foreground">0</span>
                      )}
                    </td>
                    <td className="text-right p-2">
                      <div className="flex items-center justify-end gap-1">
                        {metric.success_rate >= 80 ? (
                          <TrendingUp className="h-3 w-3 text-green-600" />
                        ) : metric.success_rate >= 50 ? (
                          <AlertCircle className="h-3 w-3 text-yellow-600" />
                        ) : (
                          <TrendingDown className="h-3 w-3 text-red-600" />
                        )}
                        <span
                          className={
                            metric.success_rate >= 80
                              ? 'text-green-600 font-medium'
                              : metric.success_rate >= 50
                                ? 'text-yellow-600'
                                : 'text-red-600'
                          }
                        >
                          {metric.success_rate.toFixed(1)}%
                        </span>
                      </div>
                    </td>
                    <td className="text-right p-2 text-sm text-muted-foreground">
                      {metric.last_escalation_at
                        ? (() => {
                          try {
                            const date = new Date(metric.last_escalation_at)
                            if (isNaN(date.getTime())) return 'Invalid date'
                            return format(date, 'MMM d, yyyy')
                          } catch {
                            return 'Invalid date'
                          }
                        })()
                        : 'Never'}
                    </td>
                    <td className="text-center p-2">
                      <Link
                        to={`/admin/app/tenants/${metric.tenant_id}`}
                        className="text-sm text-blue-600 hover:underline"
                      >
                        View Details
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>

            {(!data?.metrics || data.metrics.length === 0) && (
              <div className="py-12 text-center text-muted-foreground">
                No escalation data found for the selected period
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

