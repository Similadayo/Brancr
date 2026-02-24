import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { format } from 'date-fns'
import { FileText, User, Building } from 'lucide-react'
import { DataInterpretation } from '@/components/ui/data-interpretation'

import { useState } from 'react'
import { DateRangeFilter, DatePeriod } from '@/components/ui/date-range-filter'

interface AuditLog {
  id: string
  admin_email: string
  admin_role: string
  action: string
  method?: string
  path?: string
  tenant_id?: string
  status_code?: number
  created_at: string
}

export default function AuditLogsPage() {
  const [period, setPeriod] = useState<DatePeriod>('24h')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')

  const { data, isLoading } = useQuery<AuditLog[]>({
    queryKey: ['audit-logs', period, startDate, endDate],
    queryFn: () => api.getAuditLogs(undefined, undefined, undefined, undefined, startDate, endDate),
  })

  if (isLoading) {
    return <div className="p-6">Loading audit logs...</div>
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Audit Logs</h1>
          <p className="text-muted-foreground">
            Track all admin actions and system changes
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
        title="Understanding Audit Logs"
        type="neutral"
      >
        <p className="mb-3">
          Audit logs provide a complete record of all administrative actions and system changes for compliance, security, and troubleshooting purposes.
        </p>
        <ul className="list-disc list-inside space-y-2 ml-2 mb-3">
          <li><strong>Action:</strong> The specific action performed (e.g., "admin.user.create", "tenant.update", "escalation.resolve"). Actions are categorized by resource type and operation for easy filtering.</li>
          <li><strong>Admin Email & Role:</strong> Who performed the action and their administrative role. This helps track responsibility and detect unauthorized access patterns.</li>
          <li><strong>Tenant ID:</strong> If the action affected a specific tenant, this shows which tenant. Actions without tenant IDs are platform-wide (e.g., creating admin users, system configuration).</li>
          <li><strong>Method & Path:</strong> For API actions, shows the HTTP method (GET, POST, PUT, DELETE) and the API endpoint that was called. This helps trace actions back to specific API calls.</li>
          <li><strong>Status Code:</strong> HTTP status code returned by the action. 2xx codes indicate success, 4xx codes indicate client errors (bad requests, unauthorized), and 5xx codes indicate server errors. Failed actions (4xx/5xx) should be investigated.</li>
          <li><strong>Timestamp:</strong> Exact date and time when the action occurred, shown in your local timezone. Chronological order helps reconstruct sequences of events.</li>
        </ul>
        <p className="mb-2">
          <strong>Use Cases:</strong>
        </p>
        <ul className="list-disc list-inside space-y-1 ml-4 mb-3">
          <li><strong>Compliance:</strong> Maintain records of who accessed what data and when, required for SOC 2, GDPR, and other compliance standards.</li>
          <li><strong>Security:</strong> Detect suspicious activity patterns, unauthorized access attempts, or privilege escalations.</li>
          <li><strong>Debugging:</strong> Trace back errors or unexpected system changes to the specific admin action that caused them.</li>
          <li><strong>Accountability:</strong> Identify which admin performed specific actions, especially for sensitive operations like tenant status changes or quota adjustments.</li>
        </ul>
        <p>
          <strong>Reading the Logs:</strong> Each log entry represents a single action. If you see multiple entries for the same action, it may indicate retry attempts or multiple steps in a workflow. Status codes help quickly identify failed operations that need attention. Actions affecting tenants will show the tenant icon and tenant information when available.
        </p>
      </DataInterpretation>

      <Card>
        <CardContent className="p-0">
          <div className="divide-y">
            {data?.map((log: any) => (
              <div key={log.id} className="p-4 hover:bg-muted/50">
                <div className="flex items-start gap-3">
                  <div className="mt-1">
                    {log.tenant_id ? (
                      <Building className="h-4 w-4 text-muted-foreground" />
                    ) : (
                      <FileText className="h-4 w-4 text-muted-foreground" />
                    )}
                  </div>
                  <div className="flex-1 space-y-1">
                    <div className="flex items-center gap-2 flex-wrap">
                      <Badge variant="outline">{log.action}</Badge>
                      <Badge variant="secondary">{log.admin_role}</Badge>
                      <span className="text-sm text-muted-foreground">
                        {log.created_at ? (() => {
                          try {
                            const date = new Date(log.created_at)
                            if (isNaN(date.getTime())) return 'Invalid date'
                            return format(date, 'PPp')
                          } catch {
                            return 'Invalid date'
                          }
                        })() : 'No date'}
                      </span>
                    </div>
                    <div className="flex items-center gap-2 text-sm">
                      <User className="h-3 w-3 text-muted-foreground" />
                      <span>{log.admin_email}</span>
                      {log.path && (
                        <>
                          <span className="text-muted-foreground">•</span>
                          <span className="text-muted-foreground">{log.method} {log.path}</span>
                        </>
                      )}
                      {log.status_code && (
                        <Badge variant={log.status_code < 400 ? 'default' : 'destructive'}>
                          {log.status_code}
                        </Badge>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>

          {(!data || data.length === 0) && (
            <div className="py-12 text-center text-muted-foreground">
              No audit logs found
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

