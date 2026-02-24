import * as React from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useParams, Link } from 'react-router-dom'
import { api, type TenantBillingData } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Button } from '@/components/ui/button'
import { format } from 'date-fns'
import {
  Building2,
  Mail,
  Calendar,
  TrendingUp,
  AlertCircle,
  CheckCircle2,
  XCircle,
  Activity,
  Users,
  Ban,
  Trash2,
  PauseCircle,
  PlayCircle,
} from 'lucide-react'
import DashboardLayout from '@/components/layout/DashboardLayout'
import { useAuth } from '@/contexts/AuthContext'

export default function TenantDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { user } = useAuth()
  const tenantId = id || ''

  const { data, isLoading } = useQuery({
    queryKey: ['tenant-detail', tenantId],
    queryFn: () => api.getTenantDetail(tenantId),
    enabled: !!tenantId,
  })

  const queryClient = useQueryClient()

  // Tenant management mutations
  const blockMutation = useMutation({
    mutationFn: () => api.blockTenant(tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenant-detail', tenantId] })
      queryClient.invalidateQueries({ queryKey: ['tenants'] })
    },
  })

  const unblockMutation = useMutation({
    mutationFn: () => api.unblockTenant(tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenant-detail', tenantId] })
      queryClient.invalidateQueries({ queryKey: ['tenants'] })
    },
  })

  const suspendMutation = useMutation({
    mutationFn: () => api.suspendTenant(tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenant-detail', tenantId] })
      queryClient.invalidateQueries({ queryKey: ['tenants'] })
    },
  })

  const unsuspendMutation = useMutation({
    mutationFn: () => api.unsuspendTenant(tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenant-detail', tenantId] })
      queryClient.invalidateQueries({ queryKey: ['tenants'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: () => api.deleteTenant(tenantId),
    onSuccess: () => {
      // Redirect to tenants list after deletion
      window.location.href = '/admin/app/tenants'
    },
  })

  // Modal state
  const [deleteDialog, setDeleteDialog] = React.useState(false)
  const [blockDialog, setBlockDialog] = React.useState(false)
  const [unblockDialog, setUnblockDialog] = React.useState(false)
  const [suspendDialog, setSuspendDialog] = React.useState(false)
  const [unsuspendDialog, setUnsuspendDialog] = React.useState(false)

  if (isLoading) {
    return (
      <DashboardLayout userRole={user?.role} userEmail={user?.email}>
        <div className="p-6">Loading tenant details...</div>
      </DashboardLayout>
    )
  }

  if (!data) {
    return (
      <DashboardLayout userRole={user?.role} userEmail={user?.email}>
        <div className="p-6">
          <div className="text-center py-12">
            <AlertCircle className="h-12 w-12 text-muted-foreground mx-auto mb-4" />
            <h2 className="text-xl font-semibold mb-2">Tenant not found</h2>
            <p className="text-muted-foreground mb-4">The tenant you're looking for doesn't exist.</p>
            <Link to="/admin/app/tenants">
              <Button>Back to Tenants</Button>
            </Link>
          </div>
        </div>
      </DashboardLayout>
    )
  }

  const tenant = data.tenant
  const quota = data.quota as any
  const stats = data.stats

  const getStatusBadge = (status: string) => {
    const variants: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
      active: 'default',
      paused: 'secondary',
      suspended: 'destructive',
      inactive: 'outline',
    }
    return (
      <Badge variant={variants[status] || 'outline'}>
        {status.charAt(0).toUpperCase() + status.slice(1)}
      </Badge>
    )
  }

  return (
    <DashboardLayout userRole={user?.role} userEmail={user?.email}>
      <div className="space-y-6">
        {/* Header */}
        <div className="flex items-start justify-between">
          <div>
            <div className="flex items-center gap-3 mb-2">
              <h1 className="text-3xl font-bold tracking-tight">{tenant.name}</h1>
              {getStatusBadge(tenant.status)}
            </div>
            <div className="flex items-center gap-4 text-sm text-muted-foreground">
              <div className="flex items-center gap-1">
                <Building2 className="h-4 w-4" />
                Tenant ID: {tenant.id}
              </div>
              {tenant.email && (
                <div className="flex items-center gap-1">
                  <Mail className="h-4 w-4" />
                  {tenant.email}
                </div>
              )}
              <div className="flex items-center gap-1">
                <Calendar className="h-4 w-4" />
                Created {format(new Date(tenant.created_at), 'PP')}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            {/* Tenant Management Actions (SuperAdmin only) */}
            {user?.role === 'SuperAdmin' && (
              <>
                {/* Block/Unblock */}
                {tenant.blocked ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setUnblockDialog(true)}
                    disabled={unblockMutation.isPending}
                  >
                    <PlayCircle className="h-4 w-4 mr-1" />
                    Unblock
                  </Button>
                ) : (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setBlockDialog(true)}
                    disabled={blockMutation.isPending}
                  >
                    <Ban className="h-4 w-4 mr-1" />
                    Block
                  </Button>
                )}

                {/* Suspend/Unsuspend */}
                {tenant.suspended ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setUnsuspendDialog(true)}
                    disabled={unsuspendMutation.isPending}
                  >
                    <PlayCircle className="h-4 w-4 mr-1" />
                    Unsuspend
                  </Button>
                ) : (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setSuspendDialog(true)}
                    disabled={suspendMutation.isPending}
                  >
                    <PauseCircle className="h-4 w-4 mr-1" />
                    Suspend
                  </Button>
                )}

                {/* Delete */}
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => setDeleteDialog(true)}
                  disabled={deleteMutation.isPending}
                >
                  <Trash2 className="h-4 w-4 mr-1" />
                  Delete
                </Button>
              </>
            )}
            <Link to="/admin/app/tenants">
              <Button variant="outline">Back to Tenants</Button>
            </Link>
          </div>
        </div>

        {/* Key Metrics */}
        <div className="grid gap-4 md:grid-cols-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Interactions</CardTitle>
              <Activity className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats.total_interactions}</div>
              <p className="text-xs text-muted-foreground">
                {stats.messages_last_7_days} in last 7 days
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Escalations</CardTitle>
              <AlertCircle className="h-4 w-4 text-yellow-600" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{data.pending_escalations}</div>
              <p className="text-xs text-muted-foreground">
                {stats.escalations_count} total escalations
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Success Rate</CardTitle>
              <TrendingUp className="h-4 w-4 text-green-600" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats.success_rate.toFixed(1)}%</div>
              <p className="text-xs text-muted-foreground">
                {stats.failed_sends} failed sends
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Connected Accounts</CardTitle>
              <Users className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                {data.social_accounts.filter((a: any) => a.connected).length}
              </div>
              <p className="text-xs text-muted-foreground">
                {data.social_accounts.length} total accounts
              </p>
            </CardContent>
          </Card>
        </div>

        {/* Tenant Info & Quota */}
        <div className="grid gap-4 md:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle>Tenant Information</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <div className="text-muted-foreground">Plan Type</div>
                  <div className="font-medium">{tenant.plan_type || 'N/A'}</div>
                </div>
                <div>
                  <div className="text-muted-foreground">Industry</div>
                  <div className="font-medium">{tenant.industry || 'N/A'}</div>
                </div>
                <div>
                  <div className="text-muted-foreground">AI/Human Mode</div>
                  <div className="font-medium">{tenant.ai_human_mode || 'N/A'}</div>
                </div>
                <div>
                  <div className="text-muted-foreground">Onboarding</div>
                  <div className="font-medium">
                    {tenant.onboarding_complete ? (
                      <Badge variant="default">Complete</Badge>
                    ) : (
                      <Badge variant="secondary">{tenant.onboarding_step || 'In Progress'}</Badge>
                    )}
                  </div>
                </div>
                <div>
                  <div className="text-muted-foreground">KYB Status</div>
                  <div className="font-medium">
                    <Badge variant={tenant.kyb_status === 'verified' ? 'default' : 'secondary'}>
                      {tenant.kyb_status || 'Not Submitted'}
                    </Badge>
                  </div>
                </div>
                {stats.last_activity_at && (
                  <div>
                    <div className="text-muted-foreground">Last Activity</div>
                    <div className="font-medium">
                      {format(new Date(stats.last_activity_at), 'PPp')}
                    </div>
                  </div>
                )}
              </div>
              {tenant.admin_tags && tenant.admin_tags.length > 0 && (
                <div>
                  <div className="text-muted-foreground mb-2">Admin Tags</div>
                  <div className="flex flex-wrap gap-1">
                    {tenant.admin_tags.map((tag: string) => (
                      <Badge key={tag} variant="outline">
                        {tag}
                      </Badge>
                    ))}
                  </div>
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Quota Usage</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              {quota ? (
                <>
                  <QuotaBar
                    label="AI Calls"
                    used={quota.ai_calls_used ?? 0}
                    limit={quota.ai_calls_limit ?? 0}
                  />
                  <QuotaBar
                    label="Posts"
                    used={quota.posts_used ?? 0}
                    limit={quota.posts_limit ?? 0}
                  />
                  <QuotaBar
                    label="Storage"
                    used={quota.storage_used_mb ?? 0}
                    limit={quota.storage_limit_mb ?? 0}
                    suffix="MB"
                  />
                  <QuotaBar
                    label="API Calls"
                    used={quota.api_calls_used ?? 0}
                    limit={quota.api_calls_limit ?? 0}
                  />
                </>
              ) : (
                <div className="text-sm text-muted-foreground">No quota data available</div>
              )}
            </CardContent>
          </Card>

          {/* Billing & Plan */}
          <TenantBillingSection tenantId={tenantId} userRole={user?.role} />
        </div>

        {/* Social Accounts */}
        <Card>
          <CardHeader>
            <CardTitle>Social Accounts</CardTitle>
            <CardDescription>Connected social media platforms</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid gap-4 md:grid-cols-2">
              {data.social_accounts.map((account: any) => (
                <div
                  key={account.id}
                  className="p-4 border rounded-lg flex items-center justify-between"
                >
                  <div>
                    <div className="font-medium">{account.platform}</div>
                    <div className="text-sm text-muted-foreground">{account.username}</div>
                    {account.expires_at && (
                      <div className="text-xs text-muted-foreground mt-1">
                        Expires: {format(new Date(account.expires_at), 'PP')}
                      </div>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    {account.connected ? (
                      <Badge variant="default">
                        <CheckCircle2 className="h-3 w-3 mr-1" />
                        Connected
                      </Badge>
                    ) : (
                      <Badge variant="secondary">
                        <XCircle className="h-3 w-3 mr-1" />
                        Disconnected
                      </Badge>
                    )}
                  </div>
                </div>
              ))}
              {data.social_accounts.length === 0 && (
                <div className="col-span-2 text-center py-8 text-muted-foreground">
                  No social accounts connected
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Scheduled Posts */}
        {data.scheduled_posts && data.scheduled_posts.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Scheduled Posts</CardTitle>
              <CardDescription>Upcoming and recent scheduled posts</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                {data.scheduled_posts.slice(0, 10).map((post: any) => (
                  <div key={post.id} className="p-3 border rounded-lg">
                    <div className="flex items-center justify-between">
                      <div>
                        <div className="font-medium">
                          {post.platforms.join(', ')} • {format(new Date(post.scheduled_at), 'PPp')}
                        </div>
                        <Badge variant={post.status === 'scheduled' ? 'default' : 'secondary'}>
                          {post.status}
                        </Badge>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        )}
      </div>
      {/* Confirmation Dialogs */}
      <ConfirmDialog
        isOpen={deleteDialog}
        onClose={() => setDeleteDialog(false)}
        onConfirm={() => {
          deleteMutation.mutate()
          setDeleteDialog(false)
        }}
        title="⚠️ Permanent Delete"
        description={`This will PERMANENTLY delete ${tenant.name} and ALL associated data:\n\n• Customers\n• Orders\n• Payments\n• Social accounts\n• Scheduled posts\n• All other data\n\nThis action CANNOT be undone!`}
        confirmText="Delete Permanently"
        variant="destructive"
        requiresTyping
        typingConfirmation="DELETE"
      />

      <ConfirmDialog
        isOpen={blockDialog}
        onClose={() => setBlockDialog(false)}
        onConfirm={() => {
          blockMutation.mutate()
          setBlockDialog(false)
        }}
        title="Block Tenant"
        description={`Block ${tenant.name}?\n\nThey will lose access to the platform immediately.`}
        confirmText="Block"
        variant="destructive"
      />

      <ConfirmDialog
        isOpen={unblockDialog}
        onClose={() => setUnblockDialog(false)}
        onConfirm={() => {
          unblockMutation.mutate()
          setUnblockDialog(false)
        }}
        title="Unblock Tenant"
        description={`Unblock ${tenant.name}?\n\nThey will regain access to the platform.`}
        confirmText="Unblock"
      />

      <ConfirmDialog
        isOpen={suspendDialog}
        onClose={() => setSuspendDialog(false)}
        onConfirm={() => {
          suspendMutation.mutate()
          setSuspendDialog(false)
        }}
        title="Suspend Tenant"
        description={`Suspend ${tenant.name}?\n\nThis is a temporary restriction.`}
        confirmText="Suspend"
        variant="destructive"
      />

      <ConfirmDialog
        isOpen={unsuspendDialog}
        onClose={() => setUnsuspendDialog(false)}
        onConfirm={() => {
          unsuspendMutation.mutate()
          setUnsuspendDialog(false)
        }}
        title="Remove Suspension"
        description={`Remove suspension from ${tenant.name}?`}
        confirmText="Unsuspend"
      />
    </DashboardLayout>
  )
}

function QuotaBar({
  label,
  used,
  limit,
  suffix = '',
  percent: providedPercent,
  status,
}: {
  label: string
  used: number | undefined | null
  limit: number | undefined | null
  suffix?: string
  percent?: number
  status?: string
}) {
  const usedValue = used || 0
  const limitValue = limit || 0
  const percent = providedPercent !== undefined ? providedPercent : (limitValue > 0 ? (usedValue / limitValue) * 100 : 0)
  const isWarning = percent >= 70 && percent < 90
  const isDanger = percent >= 90
  const isExceeded = percent >= 100

  const getStatusColor = () => {
    if (status) {
      switch (status) {
        case 'exceeded':
          return 'text-red-600'
        case 'critical':
          return 'text-orange-600'
        case 'warning':
          return 'text-yellow-600'
        default:
          return 'text-green-600'
      }
    }
    return isExceeded ? 'text-red-600' : isDanger ? 'text-orange-600' : isWarning ? 'text-yellow-600' : 'text-muted-foreground'
  }

  const getBarColor = () => {
    if (status) {
      switch (status) {
        case 'exceeded':
          return 'bg-red-600'
        case 'critical':
          return 'bg-orange-600'
        case 'warning':
          return 'bg-yellow-600'
        default:
          return 'bg-blue-600'
      }
    }
    return isExceeded ? 'bg-red-600' : isDanger ? 'bg-orange-600' : isWarning ? 'bg-yellow-600' : 'bg-blue-600'
  }

  return (
    <div>
      <div className="flex justify-between text-sm mb-1">
        <span className="font-medium">{label}</span>
        <span className={getStatusColor()}>
          {usedValue.toLocaleString()} / {limitValue > 0 ? limitValue.toLocaleString() : '∞'} {suffix}
          {percent > 0 && <span className="ml-1">({percent.toFixed(1)}%)</span>}
        </span>
      </div>
      <div className="w-full bg-muted rounded-full h-2">
        <div
          className={`h-2 rounded-full transition-all ${getBarColor()}`}
          style={{ width: `${Math.min(percent, 100)}%` }}
        />
      </div>
    </div>
  )
}

function TenantBillingSection({ tenantId, userRole }: { tenantId: string; userRole?: string }) {
  const queryClient = useQueryClient()
  const { data: billing, isLoading } = useQuery<TenantBillingData>({
    queryKey: ['tenant-billing', tenantId],
    queryFn: () => api.getTenantBilling(tenantId),
    enabled: !!tenantId,
  })

  const { data: plans } = useQuery({
    queryKey: ['plan-templates'],
    queryFn: () => api.getPlanTemplates(),
  })

  const assignPlanMutation = useMutation({
    mutationFn: (data: { plan_template_id: string; override_price?: number }) =>
      api.assignPlanToTenant(tenantId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenant-billing', tenantId] })
      queryClient.invalidateQueries({ queryKey: ['tenant-detail', tenantId] })
    },
  })

  const pauseMutation = useMutation({
    mutationFn: () => api.pauseTenantPlan(tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenant-billing', tenantId] })
    },
  })

  const resumeMutation = useMutation({
    mutationFn: () => api.resumeTenantPlan(tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenant-billing', tenantId] })
    },
  })

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Billing & Plan</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-sm text-muted-foreground">Loading billing information...</div>
        </CardContent>
      </Card>
    )
  }

  if (!billing?.plan) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Billing & Plan</CardTitle>
          <CardDescription>No plan assigned</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div className="text-sm text-muted-foreground mb-4">
              This tenant doesn't have a plan assigned.
              {userRole === 'SuperAdmin' && ' Assign a plan to enable billing and quotas.'}
            </div>
            {userRole === 'SuperAdmin' && plans && plans.length > 0 && (
              <div className="space-y-2">
                <label className="block text-sm font-medium">Assign Plan</label>
                <select
                  className="w-full p-2 border rounded"
                  aria-label="Select a plan to assign"
                  onChange={(e) => {
                    const planId = e.target.value
                    if (planId) {
                      assignPlanMutation.mutate({ plan_template_id: planId })
                    }
                  }}
                  defaultValue=""
                >
                  <option value="">Select a plan...</option>
                  {plans
                    .filter((p) => p.status === 'active')
                    .map((plan) => (
                      <option key={plan.id} value={plan.id}>
                        {plan.name} - ₦{plan.default_monthly_price.toLocaleString()}/month
                      </option>
                    ))}
                </select>
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    )
  }

  const plan = billing.plan
  const usage = billing.usage

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle>Billing & Plan</CardTitle>
            <CardDescription>
              {plan.plan_name} • {plan.billing_status}
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            {userRole === 'SuperAdmin' && (
              plan.billing_status === 'paused' ? (
                <Button
                  size="sm"
                  onClick={() => resumeMutation.mutate()}
                  disabled={resumeMutation.isPending}
                >
                  Resume
                </Button>
              ) : (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    if (confirm('Pause this tenant\'s plan?')) {
                      pauseMutation.mutate()
                    }
                  }}
                  disabled={pauseMutation.isPending}
                >
                  Pause
                </Button>
              )
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <div className="text-muted-foreground">Monthly Price</div>
            <div className="font-semibold">
              ₦{plan.monthly_price.toLocaleString()}
              {plan.price_overridden && (
                <Badge variant="outline" className="ml-2">Overridden</Badge>
              )}
            </div>
          </div>
          <div>
            <div className="text-muted-foreground">Payment Status</div>
            <div className="font-semibold">{plan.payment_status}</div>
          </div>
          {plan.next_billing_date && (
            <div>
              <div className="text-muted-foreground">Next Billing</div>
              <div className="font-semibold">
                {format(new Date(plan.next_billing_date), 'PP')}
              </div>
            </div>
          )}
          {plan.trial_ends_at && (
            <div>
              <div className="text-muted-foreground">Trial Ends</div>
              <div className="font-semibold">
                {format(new Date(plan.trial_ends_at), 'PP')}
              </div>
            </div>
          )}
        </div>

        {usage && (
          <div className="space-y-3 pt-4 border-t">
            <h4 className="font-semibold text-sm">Daily Usage</h4>
            <QuotaBar
              label="AI Replies"
              used={usage.daily.ai_reply.used}
              limit={usage.daily.ai_reply.limit}
              percent={usage.daily.ai_reply.percent}
              status={usage.daily.ai_reply.status}
            />
            <QuotaBar
              label="WhatsApp Messages"
              used={usage.daily.whatsapp_message.used}
              limit={usage.daily.whatsapp_message.limit}
              percent={usage.daily.whatsapp_message.percent}
              status={usage.daily.whatsapp_message.status}
            />
            <QuotaBar
              label="Posts"
              used={usage.daily.post.used}
              limit={usage.daily.post.limit}
              percent={usage.daily.post.percent}
              status={usage.daily.post.status}
            />
            <div className="pt-2 text-sm">
              <div className="text-muted-foreground">Monthly Cost</div>
              <div className="font-semibold">
                ${usage.monthly.total_cost_usd.toFixed(2)} USD
              </div>
            </div>
          </div>
        )}

        <div className="pt-4 border-t">
          <div className="text-sm text-muted-foreground mb-2">Plan Features</div>
          <div className="flex flex-wrap gap-2">
            {plan.ai_negotiation_enabled && (
              <Badge variant="outline">AI Negotiation</Badge>
            )}
            {plan.overages_allowed && (
              <Badge variant="outline">Overages Allowed</Badge>
            )}
            <Badge variant="outline">{plan.escalation_behavior}</Badge>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

