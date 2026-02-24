import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { format } from 'date-fns'
import { AlertCircle, Info, AlertTriangle, Send, Users, Filter, Search, X } from 'lucide-react'
import { useState } from 'react'

interface Alert {
  id: string
  tenant_id: string
  tenant_name?: string
  type: string
  severity: string
  title: string
  message: string
  action_url?: string
  action_label?: string
  sent_email: boolean
  sent_telegram: boolean
  sent_in_app: boolean
  created_at: string
  read_at?: string
}

type AlertMode = 'individual' | 'bulk'

export default function AlertsPage() {
  const [showSendForm, setShowSendForm] = useState(false)
  const [alertMode, setAlertMode] = useState<AlertMode>('individual')
  const [selectedTenantIds, setSelectedTenantIds] = useState<string[]>([])
  const [tenantSearch, setTenantSearch] = useState('')
  const [filterTenantId, setFilterTenantId] = useState<string>('')
  const [formData, setFormData] = useState({
    plan_type: '',
    status: 'active',
    type: 'system',
    severity: 'info',
    title: '',
    message: '',
    action_url: '',
    action_label: '',
  })
  const queryClient = useQueryClient()

  // Fetch tenants for selection
  const { data: tenantsData } = useQuery({
    queryKey: ['tenants', 'all'],
    queryFn: () => api.getTenants('', '', 1000, 0),
  })

  // Fetch tenants by plan type for bulk alerts
  const { data: bulkTenantsData } = useQuery({
    queryKey: ['tenants-by-plan', formData.plan_type, formData.status],
    queryFn: () => api.getTenantsByPlanType(formData.plan_type || undefined, formData.status),
    enabled: alertMode === 'bulk' && (!!formData.plan_type || !!formData.status),
  })

  const { data: alerts } = useQuery({
    queryKey: ['alerts', filterTenantId],
    queryFn: () => api.getAlerts(filterTenantId || undefined),
  })

  const sendMutation = useMutation({
    mutationFn: (data: typeof formData & { tenant_ids?: string[]; plan_type?: string; status?: string }) => {
      const payload: any = {
        type: data.type,
        severity: data.severity,
        title: data.title,
        message: data.message,
      }

      if (data.action_url) payload.action_url = data.action_url
      if (data.action_label) payload.action_label = data.action_label

      if (alertMode === 'individual') {
        if (selectedTenantIds.length > 0) {
          payload.tenant_ids = selectedTenantIds
        }
      } else {
        // Bulk mode - use plan_type and status filters
        if (data.plan_type) payload.plan_type = data.plan_type
        if (data.status) payload.status = data.status
      }

      return api.sendAlert(payload)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['alerts'] })
      setShowSendForm(false)
      setSelectedTenantIds([])
      setFormData({
        plan_type: '',
        status: 'active',
        type: 'system',
        severity: 'info',
        title: '',
        message: '',
        action_url: '',
        action_label: '',
      })
    },
  })

  const getSeverityBadge = (severity: string) => {
    const variants: Record<string, { variant: 'default' | 'secondary' | 'destructive' | 'outline'; icon: React.ReactNode }> = {
      info: { variant: 'default', icon: <Info className="h-3 w-3" /> },
      warning: { variant: 'secondary', icon: <AlertTriangle className="h-3 w-3" /> },
      critical: { variant: 'destructive', icon: <AlertCircle className="h-3 w-3" /> },
    }
    const config = variants[severity] || variants.info
    return (
      <Badge variant={config.variant} className="flex items-center gap-1">
        {config.icon}
        {severity}
      </Badge>
    )
  }

  const filteredTenants = tenantsData?.items?.filter(t =>
    t.name.toLowerCase().includes(tenantSearch.toLowerCase()) ||
    t.email.toLowerCase().includes(tenantSearch.toLowerCase())
  ) || []

  const getTenantName = (tenantId: string | undefined) => {
    if (!tenantId) return 'Unknown Tenant'
    const tenant = tenantsData?.items?.find(t => t.id === tenantId)
    return tenant?.name || `Tenant #${tenantId}`
  }

  const toggleTenantSelection = (tenantId: string) => {
    setSelectedTenantIds(prev =>
      prev.includes(tenantId)
        ? prev.filter(id => id !== tenantId)
        : [...prev, tenantId]
    )
  }

  const targetCount = alertMode === 'individual'
    ? selectedTenantIds.length
    : (bulkTenantsData?.total || 0)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Alerts</h1>
          <p className="text-muted-foreground">
            Send alerts and notifications to individual tenants or groups
          </p>
        </div>
        <Button onClick={() => setShowSendForm(!showSendForm)}>
          <Send className="h-4 w-4 mr-2" />
          Send Alert
        </Button>
      </div>

      {showSendForm && (
        <Card>
          <CardHeader>
            <CardTitle>Send New Alert</CardTitle>
            <CardDescription>
              Choose to send to individual tenants or bulk/grouped by plan type
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form
              onSubmit={(e) => {
                e.preventDefault()
                sendMutation.mutate(formData)
              }}
              className="space-y-4"
            >
              {/* Mode Selection */}
              <div>
                <label className="text-sm font-medium mb-2 block">Send Mode</label>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant={alertMode === 'individual' ? 'default' : 'outline'}
                    onClick={() => {
                      setAlertMode('individual')
                      setSelectedTenantIds([])
                    }}
                    className="flex-1"
                  >
                    <Users className="h-4 w-4 mr-2" />
                    Individual Tenants
                  </Button>
                  <Button
                    type="button"
                    variant={alertMode === 'bulk' ? 'default' : 'outline'}
                    onClick={() => {
                      setAlertMode('bulk')
                      setSelectedTenantIds([])
                    }}
                    className="flex-1"
                  >
                    <Filter className="h-4 w-4 mr-2" />
                    Bulk/Grouped
                  </Button>
                </div>
              </div>

              {alertMode === 'individual' ? (
                <>
                  {/* Individual Tenant Selection */}
                  <div>
                    <label className="text-sm font-medium mb-2 block">
                      Select Tenants {selectedTenantIds.length > 0 && `(${selectedTenantIds.length} selected)`}
                    </label>
                    <div className="relative">
                      <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
                      <Input
                        type="text"
                        placeholder="Search tenants..."
                        value={tenantSearch}
                        onChange={(e) => setTenantSearch(e.target.value)}
                        className="pl-8"
                      />
                    </div>
                    <div className="mt-2 max-h-48 overflow-y-auto border rounded p-2 space-y-1">
                      {filteredTenants.slice(0, 50).map((tenant) => (
                        <label
                          key={tenant.id}
                          className="flex items-center gap-2 p-2 hover:bg-muted rounded cursor-pointer"
                        >
                          <input
                            type="checkbox"
                            checked={selectedTenantIds.includes(tenant.id)}
                            onChange={() => toggleTenantSelection(tenant.id)}
                            className="rounded"
                          />
                          <span className="text-sm">{tenant.name}</span>
                          <Badge variant="outline" className="ml-auto">{tenant.plan_type}</Badge>
                        </label>
                      ))}
                      {filteredTenants.length === 0 && (
                        <div className="text-sm text-muted-foreground p-2">No tenants found</div>
                      )}
                    </div>
                    {selectedTenantIds.length > 0 && (
                      <div className="mt-2 flex flex-wrap gap-2">
                        {selectedTenantIds.map(id => (
                          <Badge key={id} variant="secondary" className="flex items-center gap-1">
                            {getTenantName(id)}
                            <X
                              className="h-3 w-3 cursor-pointer"
                              onClick={() => toggleTenantSelection(id)}
                            />
                          </Badge>
                        ))}
                      </div>
                    )}
                    <p className="text-xs text-muted-foreground mt-1">
                      Leave empty to send to all active tenants
                    </p>
                  </div>
                </>
              ) : (
                <>
                  {/* Bulk/Grouped Selection */}
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="text-sm font-medium mb-2 block">Plan Type</label>
                      <select
                        className="w-full p-2 border rounded"
                        value={formData.plan_type}
                        onChange={(e) => setFormData({ ...formData, plan_type: e.target.value })}
                        aria-label="Plan Type"
                      >
                        <option value="">All Plans</option>
                        <option value="free">Free</option>
                        <option value="trial">Trial</option>
                        <option value="pro">Pro</option>
                        <option value="enterprise">Enterprise</option>
                      </select>
                    </div>
                    <div>
                      <label className="text-sm font-medium mb-2 block">Status</label>
                      <select
                        className="w-full p-2 border rounded"
                        value={formData.status}
                        onChange={(e) => setFormData({ ...formData, status: e.target.value })}
                        aria-label="Status"
                      >
                        <option value="">All Statuses</option>
                        <option value="active">Active</option>
                        <option value="suspended">Suspended</option>
                        <option value="pending">Pending</option>
                      </select>
                    </div>
                  </div>
                  {bulkTenantsData && bulkTenantsData.total > 0 && (
                    <div className="p-3 bg-muted rounded text-sm">
                      <strong>{bulkTenantsData.total}</strong> tenant(s) will receive this alert
                      {bulkTenantsData.total <= 10 && (
                        <div className="mt-2 text-xs">
                          {bulkTenantsData.tenants.map(t => t.name).join(', ')}
                        </div>
                      )}
                    </div>
                  )}
                </>
              )}

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-sm font-medium mb-2 block">Type</label>
                  <select
                    className="w-full p-2 border rounded"
                    aria-label="Select alert type"
                    value={formData.type}
                    onChange={(e) => setFormData({ ...formData, type: e.target.value })}
                  >
                    <option value="system">System</option>
                    <option value="billing">Billing</option>
                    <option value="quota">Quota</option>
                    <option value="security">Security</option>
                    <option value="maintenance">Maintenance</option>
                    <option value="feature">Feature</option>
                  </select>
                </div>
                <div>
                  <label className="text-sm font-medium mb-2 block">Severity</label>
                  <select
                    className="w-full p-2 border rounded"
                    aria-label="Select alert severity"
                    value={formData.severity}
                    onChange={(e) => setFormData({ ...formData, severity: e.target.value })}
                  >
                    <option value="info">Info</option>
                    <option value="warning">Warning</option>
                    <option value="critical">Critical</option>
                  </select>
                </div>
              </div>

              <div>
                <label className="text-sm font-medium mb-2 block">Title *</label>
                <Input
                  type="text"
                  value={formData.title}
                  onChange={(e) => setFormData({ ...formData, title: e.target.value })}
                  required
                />
              </div>

              <div>
                <label className="text-sm font-medium mb-2 block">Message *</label>
                <textarea
                  className="w-full min-h-[120px] p-2 border rounded"
                  value={formData.message}
                  onChange={(e) => setFormData({ ...formData, message: e.target.value })}
                  required
                />
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-sm font-medium mb-2 block">Action URL (optional)</label>
                  <Input
                    type="url"
                    value={formData.action_url}
                    onChange={(e) => setFormData({ ...formData, action_url: e.target.value })}
                  />
                </div>
                <div>
                  <label className="text-sm font-medium mb-2 block">Action Label (optional)</label>
                  <Input
                    type="text"
                    value={formData.action_label}
                    onChange={(e) => setFormData({ ...formData, action_label: e.target.value })}
                  />
                </div>
              </div>

              <div className="flex gap-2">
                <Button type="submit" disabled={sendMutation.isPending || targetCount === 0}>
                  {sendMutation.isPending ? 'Sending...' : `Send to ${targetCount} Tenant(s)`}
                </Button>
                <Button type="button" variant="outline" onClick={() => setShowSendForm(false)}>
                  Cancel
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      )}

      {/* Filter Alerts */}
      <Card>
        <CardHeader>
          <CardTitle>Filter Alerts</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex gap-4">
            <div className="flex-1">
              <label className="text-sm font-medium mb-2 block">Filter by Tenant</label>
              <select
                className="w-full p-2 border rounded"
                aria-label="Filter alerts by tenant"
                value={filterTenantId}
                onChange={(e) => setFilterTenantId(e.target.value)}
              >
                <option value="">All Tenants</option>
                {tenantsData?.items?.slice(0, 100).map((tenant) => (
                  <option key={tenant.id} value={tenant.id.toString()}>
                    {tenant.name} (#{tenant.id})
                  </option>
                ))}
              </select>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Recent Alerts</CardTitle>
          <CardDescription>Alerts sent to tenants</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {alerts?.map((alert: Alert) => (
              <div key={alert.id} className="p-4 border rounded-lg">
                <div className="flex items-start justify-between mb-2">
                  <div>
                    <div className="font-medium">{alert.title}</div>
                    <div className="text-sm text-muted-foreground">
                      {alert.tenant_name || (alert.tenant_id ? getTenantName(alert.tenant_id) : 'Unknown Tenant')} {alert.tenant_id ? `(ID: ${alert.tenant_id})` : ''} • {(() => {
                        try {
                          if (!alert.created_at) return 'No date'
                          const date = new Date(alert.created_at)
                          if (isNaN(date.getTime())) return 'Invalid date'
                          return format(date, 'PPp')
                        } catch {
                          return 'Invalid date'
                        }
                      })()}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    {getSeverityBadge(alert.severity)}
                    <Badge variant="outline">{alert.type}</Badge>
                  </div>
                </div>
                <p className="text-sm text-muted-foreground mb-2">{alert.message}</p>
                <div className="flex items-center gap-4 text-xs text-muted-foreground">
                  {alert.sent_email && <span>✓ Email</span>}
                  {alert.sent_telegram && <span>✓ Telegram</span>}
                  {alert.sent_in_app && <span>✓ In-App</span>}
                </div>
              </div>
            ))}

            {(!alerts || alerts.length === 0) && (
              <div className="text-center py-12 text-muted-foreground">
                No alerts found
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
