import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Brain, TrendingUp, AlertTriangle } from 'lucide-react'

interface AIBehaviorSectionProps {
  period: '24h' | '7d' | '30d' | 'custom'
  startDate?: string
  endDate?: string
}

export default function AIBehaviorSection({ period, startDate, endDate }: AIBehaviorSectionProps) {
  const { data, isLoading } = useQuery({
    queryKey: ['ai-behavior', period, startDate, endDate],
    queryFn: () => api.getAIBehavior(period, startDate, endDate),
  })

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>AI Behavior & Trust Signals</CardTitle>
          <CardDescription>Loading...</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (!data) {
    return (
      <Card className="shadow-sm border-gray-200">
        <CardHeader>
          <CardTitle className="text-xl font-semibold">AI Behavior</CardTitle>
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
            <CardTitle className="text-xl font-semibold">AI Behavior</CardTitle>
            <CardDescription className="mt-1">AI performance metrics and trust indicators</CardDescription>
          </div>
          <div className="h-10 w-10 rounded-lg bg-indigo-100 flex items-center justify-center">
            <Brain className="h-5 w-5 text-indigo-600" />
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
          <div className="bg-gradient-to-br from-indigo-50/50 to-transparent rounded-lg p-4 border border-indigo-100">
            <div className="flex items-center gap-2 mb-2">
              <Brain className="h-4 w-4 text-indigo-500" />
              <p className="text-xs font-medium text-gray-600">AI Replies Sent</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">{data.ai_replies_sent?.toLocaleString() || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-yellow-50/50 to-transparent rounded-lg p-4 border border-yellow-100">
            <div className="flex items-center gap-2 mb-2">
              <AlertTriangle className="h-4 w-4 text-yellow-500" />
              <p className="text-xs font-medium text-gray-600">Escalations</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">{data.escalations_triggered || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-blue-50/50 to-transparent rounded-lg p-4 border border-blue-100">
            <div className="flex items-center gap-2 mb-2">
              <TrendingUp className="h-4 w-4 text-blue-500" />
              <p className="text-xs font-medium text-gray-600">Escalation Rate</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">
              {data.escalation_rate ? `${data.escalation_rate.toFixed(1)}%` : '0%'}
            </p>
          </div>

        </div>

        {(data.manual_overrides !== undefined || data.ai_disabled_events !== undefined || data.avg_confidence_level !== undefined) && (
          <div className="grid grid-cols-2 md:grid-cols-3 gap-4 mt-6 pt-6 border-t border-gray-200">
            <div className="bg-gradient-to-br from-slate-50/50 to-transparent rounded-lg p-4 border border-slate-100">
              <p className="text-xs font-medium text-gray-600 mb-2">Manual Overrides</p>
              <p className="text-3xl font-bold text-gray-900">{data.manual_overrides || 0}</p>
            </div>

            <div className="bg-gradient-to-br from-rose-50/50 to-transparent rounded-lg p-4 border border-rose-100">
              <p className="text-xs font-medium text-gray-600 mb-2">AI Disabled Events</p>
              <p className="text-3xl font-bold text-gray-900">{data.ai_disabled_events || 0}</p>
            </div>

            <div className="bg-gradient-to-br from-cyan-50/50 to-transparent rounded-lg p-4 border border-cyan-100">
              <p className="text-xs font-medium text-gray-600 mb-2">Avg Confidence</p>
              <p className="text-3xl font-bold text-gray-900">
                {data.avg_confidence_level ? (data.avg_confidence_level * 100).toFixed(0) + '%' : 'N/A'}
              </p>
            </div>
          </div>
        )}

        {data.interpretations && data.interpretations.length > 0 && (
          <div className="border-t border-gray-200 pt-6 mt-6">
            <h4 className="font-semibold text-base mb-3 text-gray-900">Interpretations</h4>
            <ul className="space-y-2">
              {data.interpretations.map((interpretation: string, idx: number) => (
                <li key={idx} className="flex items-start gap-2 text-sm p-3 bg-gray-50 rounded-lg border border-gray-200">
                  <span className="text-gray-400 mt-0.5">•</span>
                  <span className="text-gray-700">{interpretation}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

