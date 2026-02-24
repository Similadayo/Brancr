import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type PlanTemplate } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Plus,
  Edit,
  Trash2,
  CheckCircle,
  XCircle
} from 'lucide-react'

export default function PlansPage() {
  const [showCreateForm, setShowCreateForm] = useState(false)
  const [editingPlan, setEditingPlan] = useState<PlanTemplate | null>(null)
  const queryClient = useQueryClient()

  const { data: plans, isLoading } = useQuery<PlanTemplate[]>({
    queryKey: ['plan-templates'],
    queryFn: () => api.getPlanTemplates(),
  })

  const createMutation = useMutation({
    mutationFn: (data: Parameters<typeof api.createPlanTemplate>[0]) => api.createPlanTemplate(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plan-templates'] })
      setShowCreateForm(false)
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<PlanTemplate> }) =>
      api.updatePlanTemplate(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plan-templates'] })
      setEditingPlan(null)
    },
  })

  const deprecateMutation = useMutation({
    mutationFn: (id: string) => api.deprecatePlanTemplate(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plan-templates'] })
    },
  })

  const getStatusBadge = (status: string) => {
    if (status === 'active') {
      return <Badge variant="default" className="flex items-center gap-1">
        <CheckCircle className="h-3 w-3" />
        Active
      </Badge>
    }
    return <Badge variant="secondary" className="flex items-center gap-1">
      <XCircle className="h-3 w-3" />
      Deprecated
    </Badge>
  }

  const getEscalationBadge = (behavior: string) => {
    const variants: Record<string, { variant: 'default' | 'secondary' | 'outline', label: string }> = {
      always_on: { variant: 'default', label: 'Always On' },
      configurable: { variant: 'secondary', label: 'Configurable' },
      advanced: { variant: 'outline', label: 'Advanced' },
    }
    const config = variants[behavior] || { variant: 'secondary' as const, label: behavior }
    return <Badge variant={config.variant}>{config.label}</Badge>
  }

  if (isLoading) {
    return <div className="text-center py-8">Loading plans...</div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-gray-900">Plan Templates</h1>
          <p className="text-sm sm:text-base text-gray-600 mt-1">
            Manage subscription plan templates ({plans?.length || 0} total)
          </p>
        </div>
        <Button onClick={() => setShowCreateForm(true)}>
          <Plus className="h-4 w-4 mr-2" />
          Create Plan
        </Button>
      </div>

      {/* Plans List */}
      <div className="grid gap-4">
        {plans?.map((plan) => (
          <div key={plan.id} className="border rounded-lg p-4 space-y-3">
            <div className="flex items-start justify-between">
              <div className="flex-1">
                <div className="flex items-center gap-3 mb-2">
                  <h3 className="text-lg font-semibold">{plan.name}</h3>
                  {getStatusBadge(plan.status)}
                  {plan.ai_negotiation_enabled && (
                    <Badge variant="outline">AI Negotiation</Badge>
                  )}
                  {plan.overages_allowed && (
                    <Badge variant="outline">Overages Allowed</Badge>
                  )}
                </div>
                {plan.description && (
                  <p className="text-sm text-gray-600 mb-3">{plan.description}</p>
                )}

                <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
                  <div>
                    <div className="text-gray-500">AI Replies</div>
                    <div className="font-semibold">
                      {plan.default_ai_reply_quota === 0 ? 'Unlimited' : plan.default_ai_reply_quota.toLocaleString()}
                    </div>
                  </div>
                  <div>
                    <div className="text-gray-500">WhatsApp Messages</div>
                    <div className="font-semibold">
                      {plan.default_whatsapp_message_quota === 0 ? 'Unlimited' : plan.default_whatsapp_message_quota.toLocaleString()}
                    </div>
                  </div>
                  <div>
                    <div className="text-gray-500">Posts</div>
                    <div className="font-semibold">
                      {plan.default_post_quota === 0 ? 'Unlimited' : plan.default_post_quota.toLocaleString()}
                    </div>
                  </div>
                  <div>
                    <div className="text-gray-500">Monthly Price</div>
                    <div className="font-semibold">
                      ₦{plan.default_monthly_price.toLocaleString()}
                    </div>
                  </div>
                </div>

                <div className="mt-3 flex items-center gap-4 text-sm text-gray-600">
                  <span>Escalation: {getEscalationBadge(plan.escalation_behavior)}</span>
                  <span>Storage: {plan.default_storage_limit_mb}MB</span>
                  <span>Platforms: {plan.default_platforms_limit}</span>
                </div>
              </div>

              <div className="flex items-center gap-2">
                {plan.status === 'active' && (
                  <>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setEditingPlan(plan)}
                    >
                      <Edit className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        if (confirm(`Deprecate ${plan.name}? New tenants won't be able to use this plan.`)) {
                          deprecateMutation.mutate(plan.id)
                        }
                      }}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </>
                )}
              </div>
            </div>
          </div>
        ))}
      </div>

      {/* Create/Edit Form Modal */}
      {(showCreateForm || editingPlan) && (
        <PlanForm
          plan={editingPlan}
          onClose={() => {
            setShowCreateForm(false)
            setEditingPlan(null)
          }}
          onSubmit={(data) => {
            if (editingPlan) {
              updateMutation.mutate({ id: editingPlan.id, data })
            } else {
              createMutation.mutate(data)
            }
          }}
          isLoading={createMutation.isPending || updateMutation.isPending}
        />
      )}
    </div>
  )
}

function PlanForm({
  plan,
  onClose,
  onSubmit,
  isLoading,
}: {
  plan: PlanTemplate | null
  onClose: () => void
  onSubmit: (data: Parameters<typeof api.createPlanTemplate>[0]) => void
  isLoading: boolean
}) {
  const [formData, setFormData] = useState({
    name: plan?.name || '',
    default_ai_reply_quota: plan?.default_ai_reply_quota || 0,
    default_whatsapp_message_quota: plan?.default_whatsapp_message_quota || 0,
    default_post_quota: plan?.default_post_quota || 0,
    default_storage_limit_mb: plan?.default_storage_limit_mb || 1000,
    default_api_calls_limit: plan?.default_api_calls_limit || 1000,
    default_platforms_limit: plan?.default_platforms_limit || 5,
    ai_negotiation_enabled: plan?.ai_negotiation_enabled || false,
    escalation_behavior: plan?.escalation_behavior || 'always_on',
    overages_allowed: plan?.overages_allowed || false,
    default_monthly_price: plan?.default_monthly_price || 0,
    description: plan?.description || '',
  })

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg p-6 max-w-2xl w-full max-h-[90vh] overflow-y-auto">
        <h2 className="text-xl font-bold mb-4">
          {plan ? 'Edit Plan Template' : 'Create Plan Template'}
        </h2>

        <form
          onSubmit={(e) => {
            e.preventDefault()
            onSubmit(formData)
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-sm font-medium mb-1">Plan Name</label>
            <input
              type="text"
              required
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              className="w-full p-2 border rounded"
              disabled={!!plan} // Can't change name after creation
            />
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Description</label>
            <textarea
              value={formData.description}
              onChange={(e) => setFormData({ ...formData, description: e.target.value })}
              className="w-full p-2 border rounded"
              rows={2}
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-1">AI Reply Quota (0 = unlimited)</label>
              <input
                type="number"
                min="0"
                value={formData.default_ai_reply_quota}
                onChange={(e) => setFormData({ ...formData, default_ai_reply_quota: parseInt(e.target.value) || 0 })}
                className="w-full p-2 border rounded"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">WhatsApp Message Quota (0 = unlimited)</label>
              <input
                type="number"
                min="0"
                value={formData.default_whatsapp_message_quota}
                onChange={(e) => setFormData({ ...formData, default_whatsapp_message_quota: parseInt(e.target.value) || 0 })}
                className="w-full p-2 border rounded"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Post Quota (0 = unlimited)</label>
              <input
                type="number"
                min="0"
                value={formData.default_post_quota}
                onChange={(e) => setFormData({ ...formData, default_post_quota: parseInt(e.target.value) || 0 })}
                className="w-full p-2 border rounded"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Monthly Price (₦)</label>
              <input
                type="number"
                min="0"
                step="0.01"
                value={formData.default_monthly_price}
                onChange={(e) => setFormData({ ...formData, default_monthly_price: parseFloat(e.target.value) || 0 })}
                className="w-full p-2 border rounded"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Storage Limit (MB)</label>
              <input
                type="number"
                min="0"
                value={formData.default_storage_limit_mb}
                onChange={(e) => setFormData({ ...formData, default_storage_limit_mb: parseInt(e.target.value) || 0 })}
                className="w-full p-2 border rounded"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Platforms Limit</label>
              <input
                type="number"
                min="0"
                value={formData.default_platforms_limit}
                onChange={(e) => setFormData({ ...formData, default_platforms_limit: parseInt(e.target.value) || 0 })}
                className="w-full p-2 border rounded"
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Escalation Behavior</label>
            <select
              value={formData.escalation_behavior}
              onChange={(e) => setFormData({ ...formData, escalation_behavior: e.target.value })}
              className="w-full p-2 border rounded"
            >
              <option value="always_on">Always On</option>
              <option value="configurable">Configurable</option>
              <option value="advanced">Advanced</option>
            </select>
          </div>

          <div className="flex items-center gap-4">
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={formData.ai_negotiation_enabled}
                onChange={(e) => setFormData({ ...formData, ai_negotiation_enabled: e.target.checked })}
                className="rounded"
              />
              <span className="text-sm">AI Negotiation Enabled</span>
            </label>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={formData.overages_allowed}
                onChange={(e) => setFormData({ ...formData, overages_allowed: e.target.checked })}
                className="rounded"
              />
              <span className="text-sm">Overages Allowed</span>
            </label>
          </div>

          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={isLoading}>
              {isLoading ? 'Saving...' : plan ? 'Update' : 'Create'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  )
}

