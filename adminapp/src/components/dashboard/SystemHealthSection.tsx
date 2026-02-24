import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Activity, AlertCircle, CheckCircle2 } from 'lucide-react'

interface SystemHealthSectionProps {
  period: '24h' | '7d' | '30d' | 'custom'
  startDate?: string
  endDate?: string
}

export default function SystemHealthSection({ period, startDate, endDate }: SystemHealthSectionProps) {
  const { data, isLoading } = useQuery({
    queryKey: ['system-health', period, startDate, endDate],
    queryFn: () => api.getSystemHealth(period, startDate, endDate),
  })

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>System & Platform Health</CardTitle>
          <CardDescription>Loading...</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (!data) return null

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'green':
        return <CheckCircle2 className="h-5 w-5 text-green-500" />
      case 'amber':
        return <AlertCircle className="h-5 w-5 text-yellow-500" />
      case 'red':
        return <AlertCircle className="h-5 w-5 text-red-500" />
      default:
        return <Activity className="h-5 w-5 text-gray-500" />
    }
  }

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'green':
        return <Badge variant="success">Healthy</Badge>
      case 'amber':
        return <Badge variant="warning">Warning</Badge>
      case 'red':
        return <Badge variant="destructive">Critical</Badge>
      default:
        return <Badge variant="secondary">Unknown</Badge>
    }
  }

  return (
    <Card className="shadow-sm border-gray-200">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-xl font-semibold">System Health</CardTitle>
            <CardDescription className="mt-1">Platform status and system metrics</CardDescription>
          </div>
          <div className="h-10 w-10 rounded-lg bg-blue-100 flex items-center justify-center">
            <Activity className="h-5 w-5 text-blue-600" />
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        {data.platforms && data.platforms.length > 0 ? (
          <div className="space-y-3">
            {data.platforms.map((platform: any) => (
              <div key={platform.name} className="border border-gray-200 rounded-lg p-4 hover:border-gray-300 transition-colors bg-gradient-to-r from-gray-50/50 to-transparent">
                <div className="flex items-center justify-between mb-3">
                  <div className="flex items-center gap-3">
                    {getStatusIcon(platform.status)}
                    <h3 className="font-semibold text-base capitalize">{platform.name}</h3>
                  </div>
                  {getStatusBadge(platform.status)}
                </div>
                <div className="grid grid-cols-2 md:grid-cols-5 gap-4 text-sm">
                  <div className="space-y-1">
                    <p className="text-xs text-gray-500 font-medium">Received</p>
                    <p className="text-lg font-bold text-gray-900">{platform.messages_received?.toLocaleString() || 0}</p>
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs text-gray-500 font-medium">Sent</p>
                    <p className="text-lg font-bold text-gray-900">{platform.messages_sent?.toLocaleString() || 0}</p>
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs text-gray-500 font-medium">Failed</p>
                    <p className="text-lg font-bold text-red-600">{platform.failed_sends || 0}</p>
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs text-gray-500 font-medium">Retries</p>
                    <p className="text-lg font-bold text-gray-900">{platform.retry_count || 0}</p>
                  </div>
                  <div className="space-y-1">
                    <p className="text-xs text-gray-500 font-medium">API Errors</p>
                    <p className="text-lg font-bold text-red-600">{platform.api_errors || 0}</p>
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-muted-foreground">No platform data available</p>
        )}

        {data.system && (
          <div className="mt-6 pt-6 border-t border-gray-200">
            <h4 className="font-semibold text-base mb-4 text-gray-900">System Metrics</h4>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              <div className="space-y-1">
                <p className="text-xs text-gray-500 font-medium">Job Failures</p>
                <p className="text-xl font-bold text-gray-900">{data.system.background_job_failures || 0}</p>
              </div>
              <div className="space-y-1">
                <p className="text-xs text-gray-500 font-medium">Queue Backlog</p>
                <p className="text-xl font-bold text-gray-900">{data.system.queue_backlog_size || 0}</p>
              </div>
              <div className="space-y-1">
                <p className="text-xs text-gray-500 font-medium">Service Uptime</p>
                <p className="text-xl font-bold text-green-600">{data.system.service_uptime || 'N/A'}</p>
              </div>
              {data.system.last_incident_timestamp && (
                <div className="space-y-1">
                  <p className="text-xs text-gray-500 font-medium">Last Incident</p>
                  <p className="text-sm font-semibold text-gray-700">
                    {new Date(data.system.last_incident_timestamp).toLocaleDateString()}
                  </p>
                </div>
              )}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

