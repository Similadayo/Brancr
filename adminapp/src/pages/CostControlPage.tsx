import { useState, useEffect } from 'react'
import { useAuth } from '@/contexts/AuthContext'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import CostControlSection from '@/components/dashboard/CostControlSection'
import { Button } from '@/components/ui/button'
import { DataInterpretation } from '@/components/ui/data-interpretation'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { DateRangeFilter, DatePeriod } from '@/components/ui/date-range-filter'
import { Loader2, Save, Settings } from 'lucide-react'
import { api } from '@/lib/api'

export default function CostControlPage() {
  const { user } = useAuth()
  const [period, setPeriod] = useState<DatePeriod>('24h')
  const [currency, setCurrency] = useState<'USD' | 'NGN' | 'EUR' | 'GBP'>('USD')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')

  if (!user || user.role !== 'SuperAdmin') {
    return (
      <div className="text-center py-12">
        <h2 className="text-2xl font-bold text-gray-900 mb-2">Access Denied</h2>
        <p className="text-gray-600">You don't have permission to access this page.</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold text-gray-900">Cost Control</h1>
          <p className="text-sm sm:text-base text-gray-600 mt-1">AI usage costs and spending tracking</p>
        </div>
        <div className="flex flex-col gap-2">
          <div className="flex gap-2 flex-wrap items-center">
            <DateRangeFilter
              period={period}
              setPeriod={setPeriod}
              startDate={startDate}
              setStartDate={setStartDate}
              endDate={endDate}
              setEndDate={setEndDate}
            />
            <select
              value={currency}
              onChange={(e) => setCurrency(e.target.value as 'USD' | 'NGN' | 'EUR' | 'GBP')}
              className="px-3 py-1.5 text-sm border border-gray-300 rounded-md bg-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent mt-1"
            >
              <option value="USD">USD ($)</option>
              <option value="NGN">NGN (₦)</option>
              <option value="EUR">EUR (€)</option>
              <option value="GBP">GBP (£)</option>
            </select>
          </div>
        </div>
      </div>

      {/* Data Interpretation */}
      <DataInterpretation
        title="Understanding Cost Control Metrics"
        type="info"
      >
        <p className="mb-3">
          This page tracks all AI-related costs and usage across all tenants. Cost calculation works as follows:
        </p>
        <ul className="list-disc list-inside space-y-2 ml-2 mb-3">
          <li><strong>AI Cost (Estimated):</strong> Total dollar amount spent on OpenAI API calls during the selected period. This is calculated from actual token usage when available, or estimated from AI call counts. The system tracks costs in real-time using model-specific pricing (GPT-4, GPT-3.5, etc.). If you see $0.00, it means either no AI calls were made, or cost tracking hasn't been initialized yet for those calls.</li>
          <li><strong>WhatsApp Cost:</strong> Actual cost from Gupshup Partner API when available, otherwise estimated at approximately $0.0075 per message. This includes all outbound messages sent through our platform, including automated responses and tenant-initiated messages. Actual costs are synced daily from Gupshup and reflect real WhatsApp Business Platform pricing.</li>
          <li><strong>Total Cost:</strong> Combined AI costs + WhatsApp costs. This represents the total infrastructure cost for the selected period.</li>
          <li><strong>OpenAI Tokens Used:</strong> Total number of tokens processed across all AI calls (input tokens + output tokens). Each token represents approximately 4 characters of text. Higher token counts indicate more extensive AI processing. If cost_used is tracked, tokens are calculated from cost. If not, tokens are estimated from AI call counts (~2000 tokens per call on average).</li>
          <li><strong>Cost Per Tenant:</strong> Detailed breakdown showing AI costs, WhatsApp costs, and total costs for each tenant. Displays AI cost (from OpenAI API usage), WhatsApp cost (from outbound messages sent), total combined cost, and WhatsApp message count. Tenants are sorted by total cost (highest first). This helps identify which tenants are driving the highest infrastructure costs.</li>
          <li><strong>Top Cost Tenants:</strong> The 10 tenants with the highest costs during the period. Monitor these closely as they represent the highest infrastructure usage.</li>
        </ul>
        <p className="mb-2">
          <strong>Cost Calculation Method:</strong> When actual cost data is available (cost_used greater than 0 in tenant_quotas), we use those values. If cost_used is 0 but AI calls were made, we estimate costs by assuming ~2000 tokens per call at ~$0.000045 per token (average GPT-4 pricing), resulting in approximately $0.09 per AI call. WhatsApp costs use actual Gupshup Partner API costs when synced data is available, otherwise estimated at $0.0075 per message based on WhatsApp Business Platform pricing.
        </p>
        <p className="mb-2">
          <strong>Alerts:</strong> The system generates alerts for:
        </p>
        <ul className="list-disc list-inside space-y-1 ml-4 mb-3">
          <li><strong>Threshold alerts:</strong> Tenants that have exceeded their configured cost limits</li>
          <li><strong>Outlier alerts:</strong> Tenants with unusually high costs compared to the average (3x the average cost)</li>
        </ul>
        <p>
          <strong>Action Items:</strong> If costs appear higher than expected, review the top cost tenants and their usage patterns. Consider setting cost limits for tenants that consistently exceed thresholds. If you see $0.00 costs but AI calls are being made, verify that the cost tracking system is properly initialized and that IncrementAICallCost is being called after each AI operation.
        </p>
      </DataInterpretation>

      {/* Cost Control Settings */}
      <CostControlSettingsCard />

      {/* Cost Data */}
      <CostControlSection
        period={period}
        currency={currency}
        startDate={startDate}
        endDate={endDate}
      />
    </div>
  )
}


function CostControlSettingsCard() {
  const queryClient = useQueryClient()
  const [maxAICallsPerMin, setMaxAICallsPerMin] = useState<number>(3)

  const { data: settings, isLoading } = useQuery({
    queryKey: ['cost-control-settings'],
    queryFn: () => api.getCostControlSettings(),
  })

  const updateMutation = useMutation({
    mutationFn: (settings: { max_ai_calls_per_min: number }) =>
      api.updateCostControlSettings(settings),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cost-control-settings'] })
      alert('Cost control settings updated successfully')
    },
    onError: (error: Error) => {
      alert(`Failed to update settings: ${error.message}`)
    },
  })

  useEffect(() => {
    if (settings) {
      setMaxAICallsPerMin(settings.max_ai_calls_per_min)
    }
  }, [settings])

  const handleSave = () => {
    if (maxAICallsPerMin < 1 || maxAICallsPerMin > 10) {
      alert('Max AI calls per minute must be between 1 and 10')
      return
    }
    updateMutation.mutate({ max_ai_calls_per_min: maxAICallsPerMin })
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <Settings className="h-5 w-5 text-gray-600" />
          <div>
            <CardTitle>Cost Control Settings</CardTitle>
            <CardDescription>
              Configure AI call limits per conversation to control costs
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-6 w-6 animate-spin text-gray-400" />
            <span className="ml-2 text-sm text-gray-600">Loading settings...</span>
          </div>
        ) : (
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="max-ai-calls">
                Max AI Calls Per Conversation Per Minute
              </Label>
              <Input
                id="max-ai-calls"
                type="number"
                min="1"
                max="10"
                value={maxAICallsPerMin}
                onChange={(e) => setMaxAICallsPerMin(parseInt(e.target.value) || 3)}
                className="max-w-xs"
              />
              <p className="text-sm text-gray-500">
                Limits the number of AI calls per conversation per minute. When exceeded, a friendly fallback message is sent instead. Recommended: 3-5 calls per minute.
              </p>
            </div>
            <Button
              onClick={handleSave}
              disabled={updateMutation.isPending}
              className="w-full sm:w-auto"
            >
              {updateMutation.isPending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Saving...
                </>
              ) : (
                <>
                  <Save className="h-4 w-4 mr-2" />
                  Save Settings
                </>
              )}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

