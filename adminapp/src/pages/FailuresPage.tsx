import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { format } from 'date-fns'
import { AlertTriangle, MessageSquare, Image, CreditCard, Bot, Clock, Globe, Key } from 'lucide-react'
import { DataInterpretation } from '@/components/ui/data-interpretation'
import { Badge } from '@/components/ui/badge'

interface FailuresData {
  summary: {
    total_failures: number
    failed_messages: number
    failed_posts: number
    failed_payments: number
    failed_escalations: number
    failed_integrations: number
    failed_scheduled: number
    failed_webhooks: number
    failed_tokens: number
    failed_api_requests: number
    error_rate: number
  }
  failed_messages: any[]
  failed_posts: any[]
  failed_payments: any[]
  failed_escalations: any[]
  failed_integrations: any[]
  failed_scheduled_posts: any[]
  failed_webhooks: any[]
  failed_token_refreshes: any[]
  failed_api_requests: any[]
}

export default function FailuresPage() {
  const { data, isLoading } = useQuery<FailuresData>({
    queryKey: ['failures'],
    queryFn: () => api.getFailures(),
  })

  if (isLoading) {
    return <div className="p-6">Loading failures...</div>
  }

  const failures = data as FailuresData
  const summary = failures?.summary || {}

  const failureStats = [
    { label: 'Messages', value: summary.failed_messages || 0, icon: MessageSquare, color: 'text-blue-600' },
    { label: 'Posts', value: summary.failed_posts || 0, icon: Image, color: 'text-purple-600' },
    { label: 'Payments', value: summary.failed_payments || 0, icon: CreditCard, color: 'text-green-600' },
    { label: 'Escalations', value: summary.failed_escalations || 0, icon: AlertTriangle, color: 'text-red-600' },
    { label: 'Integrations', value: summary.failed_integrations || 0, icon: Bot, color: 'text-indigo-600' },
    { label: 'Scheduled', value: summary.failed_scheduled || 0, icon: Clock, color: 'text-orange-600' },
    { label: 'Webhooks', value: summary.failed_webhooks || 0, icon: Globe, color: 'text-cyan-600' },
    { label: 'Tokens', value: summary.failed_tokens || 0, icon: Key, color: 'text-yellow-600' },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">System Failures</h1>
        <p className="text-muted-foreground">
          Track and monitor all system failures and errors
        </p>
      </div>

      {/* Data Interpretation */}
      <DataInterpretation 
        title="Understanding System Failures"
        type="warning"
      >
        <p className="mb-2">
          This page tracks all system failures across different components. Each failure category represents errors from specific parts of the system:
        </p>
        <ul className="list-disc list-inside space-y-1.5 mb-2">
          <li><strong>Failed Messages:</strong> Outbound messages (WhatsApp, Instagram, Facebook) that failed to send. These failures originate from the messaging service when attempting to deliver messages to customers. Check error details for API errors, network issues, or invalid recipient data.</li>
          <li><strong>Failed Posts:</strong> Social media posts that failed to publish. These occur when scheduled posts cannot be published to platforms like Instagram or Facebook. Common causes include invalid media, expired tokens, or API rate limits.</li>
          <li><strong>Failed Payments:</strong> Tenant subscription payment failures (tenant-to-Brancr payments only, not customer payments). These indicate issues with tenant billing when processing subscription renewals or payments.</li>
          <li><strong>Failed Escalations:</strong> Escalation attempts that failed to notify tenant owners via Telegram. This happens when the escalation system cannot reach the tenant's Telegram bot or the notification fails to send.</li>
          <li><strong>Failed Integrations:</strong> API integration failures with external services (Meta, WhatsApp Business API, etc.). These indicate connectivity or authentication issues with third-party platforms.</li>
          <li><strong>Failed Scheduled Posts:</strong> Posts that failed during scheduled execution. Different from regular failed posts, these specifically failed at their scheduled time.</li>
          <li><strong>Failed Webhooks:</strong> Outbound webhook deliveries that failed. This occurs when webhook notifications to tenant endpoints cannot be delivered.</li>
          <li><strong>Failed Tokens:</strong> OAuth token refresh failures. When social media platform tokens expire, the system attempts to refresh them automatically. Failures indicate re-authentication is needed.</li>
          <li><strong>Failed API Requests:</strong> Internal API request failures in the admin dashboard or tenant APIs. These show errors from API endpoints that returned 4xx or 5xx status codes.</li>
        </ul>
        <p className="mb-2">
          <strong>Error Rate:</strong> The percentage of total requests that resulted in failures. A high error rate (greater than 5%) indicates systemic issues that need immediate attention. The rate is calculated as (Total Failures / Total Requests) × 100.
        </p>
        <p>
          <strong>Failure Details:</strong> Each failure entry includes the tenant affected, platform/service where the failure occurred, timestamp, and detailed error message. Error messages include source information (where the failure originated) and context about retry attempts, if applicable.
        </p>
      </DataInterpretation>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium">Total Failures</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{summary.total_failures || 0}</div>
            <p className="text-xs text-muted-foreground mt-1">
              Error Rate: {(summary.error_rate || 0).toFixed(2)}%
            </p>
          </CardContent>
        </Card>

        {failureStats.map((stat) => {
          const Icon = stat.icon
          return (
            <Card key={stat.label}>
              <CardHeader className="pb-3">
                <CardTitle className="text-sm font-medium flex items-center gap-2">
                  <Icon className={`h-4 w-4 ${stat.color}`} />
                  {stat.label}
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{stat.value}</div>
              </CardContent>
            </Card>
          )
        })}
      </div>

      <div className="space-y-4">
        <h2 className="text-xl font-semibold">Recent Failures</h2>
        
        {failures?.failed_escalations && failures.failed_escalations.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <AlertTriangle className="h-5 w-5 text-red-600" />
                Failed Escalations ({failures.failed_escalations.length})
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                {failures.failed_escalations.slice(0, 10).map((esc: any) => (
                  <div key={esc.id} className="text-sm p-3 bg-muted rounded-lg border border-gray-200">
                    <div className="flex justify-between items-start mb-2">
                      <span className="font-semibold">{esc.tenant_name || 'Unknown Tenant'}</span>
                      <span className="text-muted-foreground text-xs">
                        {(() => {
                          try {
                            if (!esc.failed_at) return 'No date'
                            const date = new Date(esc.failed_at)
                            if (isNaN(date.getTime())) return 'Invalid date'
                            return format(date, 'PPp')
                          } catch {
                            return 'Invalid date'
                          }
                        })()}
                      </span>
                    </div>
                    <p className="text-muted-foreground font-mono text-xs">{esc.error}</p>
                    {esc.interaction_id && (
                      <p className="text-xs text-gray-500 mt-1">Interaction ID: {esc.interaction_id}</p>
                    )}
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        )}

        {failures?.failed_messages && failures.failed_messages.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Failed Messages</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                {failures.failed_messages.slice(0, 10).map((msg: any) => (
                  <div key={msg.id} className="text-sm p-3 bg-muted rounded-lg border border-gray-200">
                    <div className="flex justify-between items-start mb-2">
                      <div className="flex items-center gap-2">
                        <span className="font-semibold">{msg.tenant_name || 'Unknown Tenant'}</span>
                        <Badge variant="outline" className="text-xs">{msg.platform}</Badge>
                        <Badge variant="secondary" className="text-xs">{msg.direction}</Badge>
                      </div>
                      <span className="text-muted-foreground text-xs whitespace-nowrap ml-2">
                        {(() => {
                          try {
                            if (!msg.failed_at) return 'No date'
                            const date = new Date(msg.failed_at)
                            if (isNaN(date.getTime())) return 'Invalid date'
                            return format(date, 'PPp')
                          } catch {
                            return 'Invalid date'
                          }
                        })()}
                      </span>
                    </div>
                    <p className="text-muted-foreground font-mono text-xs mb-1">{msg.error}</p>
                    {msg.retry_count > 0 && (
                      <p className="text-xs text-amber-600">
                        Retried {msg.retry_count} time(s)
                        {msg.last_retry_at && ` • Last retry: ${(() => {
                          try {
                            const date = new Date(msg.last_retry_at)
                            if (isNaN(date.getTime())) return 'Invalid date'
                            return format(date, 'PPp')
                          } catch {
                            return 'Invalid date'
                          }
                        })()}`}
                      </p>
                    )}
                    {msg.customer_id && (
                      <p className="text-xs text-gray-500">Customer ID: {msg.customer_id}</p>
                    )}
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        )}

        {failures?.failed_posts && failures.failed_posts.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Failed Posts</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                {failures.failed_posts.slice(0, 10).map((post: any) => (
                  <div key={post.id} className="text-sm p-3 bg-muted rounded-lg border border-gray-200">
                    <div className="flex justify-between items-start mb-2">
                      <div className="flex items-center gap-2">
                        <span className="font-semibold">{post.tenant_name || 'Unknown Tenant'}</span>
                        <Badge variant="outline" className="text-xs">{post.platform}</Badge>
                        <Badge variant="destructive" className="text-xs">{post.status}</Badge>
                      </div>
                      <span className="text-muted-foreground text-xs whitespace-nowrap ml-2">
                        {(() => {
                          try {
                            if (!post.failed_at) return 'No date'
                            const date = new Date(post.failed_at)
                            if (isNaN(date.getTime())) return 'Invalid date'
                            return format(date, 'PPp')
                          } catch {
                            return 'Invalid date'
                          }
                        })()}
                      </span>
                    </div>
                    <p className="text-muted-foreground font-mono text-xs">{post.error}</p>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}

