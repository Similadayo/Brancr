import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { DollarSign, AlertCircle } from 'lucide-react'

interface CostControlSectionProps {
  period: '24h' | '7d' | '30d' | 'custom'
  currency: 'USD' | 'NGN' | 'EUR' | 'GBP'
  startDate?: string
  endDate?: string
}

// Currency conversion rates (to USD as base)
const CURRENCY_RATES: Record<string, number> = {
  USD: 1.0,
  NGN: 0.0012, // 1 USD = ~833 NGN (approximate)
  EUR: 1.08,
  GBP: 1.27,
}

// Currency symbols
const CURRENCY_SYMBOLS: Record<string, string> = {
  USD: '$',
  NGN: '₦',
  EUR: '€',
  GBP: '£',
}

// Convert USD to target currency
function convertCurrency(usdAmount: number, targetCurrency: string): number {
  const rate = CURRENCY_RATES[targetCurrency] || 1.0
  return usdAmount / rate
}

// Format currency amount
function formatCurrency(amount: number, currency: string): string {
  const symbol = CURRENCY_SYMBOLS[currency] || '$'
  if (currency === 'NGN') {
    // NGN: no decimals, use comma separators
    return `${symbol}${Math.round(amount).toLocaleString()}`
  }
  // Other currencies: 2 decimals
  return `${symbol}${amount.toFixed(2)}`
}

export default function CostControlSection({ period, currency, startDate, endDate }: CostControlSectionProps) {
  const { data, isLoading } = useQuery({
    queryKey: ['cost-control', period, startDate, endDate],
    queryFn: () => api.getCostControl(period, startDate, endDate),
  })

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Usage & Cost Control</CardTitle>
          <CardDescription>Loading...</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (!data) {
    return (
      <Card className="shadow-sm border-gray-200">
        <CardHeader>
          <CardTitle className="text-xl font-semibold">Cost Control</CardTitle>
          <CardDescription>No data available</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <Card className="shadow-sm border-gray-200">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-xl font-semibold">Cost Control</CardTitle>
            <CardDescription className="mt-1">AI usage costs and spending tracking</CardDescription>
          </div>
          <div className="h-10 w-10 rounded-lg bg-emerald-100 flex items-center justify-center">
            <DollarSign className="h-5 w-5 text-emerald-600" />
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="grid grid-cols-2 md:grid-cols-5 gap-4 mb-6">
          <div className="bg-gradient-to-br from-emerald-50/50 to-transparent rounded-lg p-4 border border-emerald-100">
            <div className="flex items-center gap-2 mb-2">
              <DollarSign className="h-4 w-4 text-emerald-500" />
              <p className="text-xs font-medium text-gray-600">AI Cost</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">
              {formatCurrency(convertCurrency(data.estimated_ai_cost || 0, currency), currency)}
            </p>
          </div>

          <div className="bg-gradient-to-br from-purple-50/50 to-transparent rounded-lg p-4 border border-purple-100">
            <div className="flex items-center gap-2 mb-2">
              <DollarSign className="h-4 w-4 text-purple-500" />
              <p className="text-xs font-medium text-gray-600">WhatsApp Cost</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">
              {formatCurrency(convertCurrency(data.whatsapp_cost || 0, currency), currency)}
            </p>
          </div>

          <div className="bg-gradient-to-br from-blue-50/50 to-transparent rounded-lg p-4 border border-blue-100 col-span-2 md:col-span-1">
            <div className="flex items-center gap-2 mb-2">
              <DollarSign className="h-4 w-4 text-blue-500" />
              <p className="text-xs font-medium text-gray-600">Total Cost</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">
              {formatCurrency(convertCurrency(data.total_cost || (data.estimated_ai_cost || 0) + (data.whatsapp_cost || 0), currency), currency)}
            </p>
          </div>

          <div className="bg-gradient-to-br from-blue-50/50 to-transparent rounded-lg p-4 border border-blue-100">
            <p className="text-xs font-medium text-gray-600 mb-2">OpenAI Tokens</p>
            <p className="text-3xl font-bold text-gray-900">{data.openai_tokens_used?.toLocaleString() || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-purple-50/50 to-transparent rounded-lg p-4 border border-purple-100">
            <p className="text-xs font-medium text-gray-600 mb-2">WhatsApp Messages</p>
            <p className="text-3xl font-bold text-gray-900">{data.whatsapp_messages_sent?.toLocaleString() || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-orange-50/50 to-transparent rounded-lg p-4 border border-orange-100">
            <p className="text-xs font-medium text-gray-600 mb-2">Active Tenants</p>
            <p className="text-3xl font-bold text-gray-900">{data.cost_per_tenant?.length || 0}</p>
          </div>
        </div>

        {data.alerts && data.alerts.length > 0 && (
          <div className="border-t pt-4 mb-6">
            <div className="flex items-center gap-2 mb-3">
              <AlertCircle className="h-5 w-5 text-yellow-500" />
              <h4 className="font-semibold">Cost Alerts</h4>
            </div>
            <div className="space-y-2">
              {data.alerts.map((alert: any, idx: number) => (
                <div key={idx} className="flex items-start gap-2 p-3 bg-yellow-50 dark:bg-yellow-900/20 rounded-lg">
                  <Badge variant={alert.type === 'threshold' ? 'destructive' : 'warning'}>
                    {alert.type}
                  </Badge>
                  <span className="text-sm">{alert.message}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {data.cost_per_tenant && data.cost_per_tenant.length > 0 && (
          <div className="border-t pt-4">
            <h4 className="font-semibold mb-3">Cost Per Tenant</h4>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-200">
                    <th className="text-left py-2 px-3 font-medium text-gray-700">Tenant</th>
                    <th className="text-right py-2 px-3 font-medium text-gray-700">AI Cost</th>
                    <th className="text-right py-2 px-3 font-medium text-gray-700">WhatsApp Cost</th>
                    <th className="text-right py-2 px-3 font-medium text-gray-700">Total Cost</th>
                    <th className="text-right py-2 px-3 font-medium text-gray-700">Messages</th>
                  </tr>
                </thead>
                <tbody>
                  {data.cost_per_tenant.slice(0, 10).map((tenant: any) => (
                    <tr key={tenant.tenant_id} className="border-b border-gray-100 hover:bg-gray-50">
                      <td className="py-2 px-3 font-medium text-gray-900">
                        {!tenant.tenant_name || tenant.tenant_name.toLowerCase() === 'unknown' ? (
                          <div className="flex flex-col">
                            <span className="text-gray-500 italic">Unknown Tenant</span>
                            <span className="text-xs text-gray-400 font-mono">
                              ID: {tenant.tenant_id ? tenant.tenant_id.slice(0, 8) + '...' : 'N/A'}
                            </span>
                          </div>
                        ) : (
                          <div title={`ID: ${tenant.tenant_id}`}>
                            {tenant.tenant_name}
                          </div>
                        )}
                      </td>
                      <td className="py-2 px-3 text-right text-gray-700">
                        {formatCurrency(convertCurrency(tenant.cost || 0, currency), currency)}
                      </td>
                      <td className="py-2 px-3 text-right text-gray-700">
                        {formatCurrency(convertCurrency(tenant.whatsapp_cost || 0, currency), currency)}
                      </td>
                      <td className="py-2 px-3 text-right font-semibold text-gray-900">
                        {formatCurrency(convertCurrency(tenant.total_cost || (tenant.cost || 0) + (tenant.whatsapp_cost || 0), currency), currency)}
                      </td>
                      <td className="py-2 px-3 text-right text-gray-600">
                        {tenant.whatsapp_messages?.toLocaleString() || 0}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {data.cost_per_tenant.length > 10 && (
              <p className="text-xs text-gray-500 mt-2 text-center">
                Showing top 10 tenants. Total: {data.cost_per_tenant.length} tenants
              </p>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

