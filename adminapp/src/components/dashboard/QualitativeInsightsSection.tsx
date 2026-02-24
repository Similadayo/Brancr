import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Lightbulb, AlertCircle, MessageSquare, Flag } from 'lucide-react'
import { format } from 'date-fns'
import type { QualitativeInsightsData } from '@/types/dashboard'

interface QualitativeInsightsSectionProps {
  period: '24h' | '7d' | '30d' | 'custom'
  startDate?: Date
  endDate?: Date
}

export default function QualitativeInsightsSection({ period, startDate, endDate }: QualitativeInsightsSectionProps) {
  const { data, isLoading } = useQuery<QualitativeInsightsData>({
    queryKey: ['qualitative-insights', period, startDate, endDate],
    queryFn: () => {
      const startStr = startDate ? startDate.toISOString().split('T')[0] : undefined
      const endStr = endDate ? endDate.toISOString().split('T')[0] : undefined
      return api.getQualitativeInsights(period, startStr, endStr)
    },
  })

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Qualitative Insights & Risk Flags</CardTitle>
          <CardDescription>Loading...</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (!data) {
    return (
      <Card className="shadow-sm border-gray-200">
        <CardHeader>
          <CardTitle className="text-xl font-semibold">Qualitative Insights</CardTitle>
          <CardDescription>No data available</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  const renderInsightList = (title: string, insights: any[], icon: React.ReactNode) => {
    if (!insights || insights.length === 0) return null

    return (
      <div>
        <div className="flex items-center gap-2 mb-3">
          {icon}
          <h4 className="font-semibold text-base text-gray-900">{title}</h4>
        </div>
        <div className="space-y-2 mb-4">
          {insights.map((insight: any) => (
            <div key={insight.id} className="p-4 border border-gray-200 rounded-lg text-sm bg-white hover:bg-gray-50 transition-colors">
              <div className="flex items-start justify-between mb-2">
                <div className="flex-1">
                  <p className="mb-1">{insight.content}</p>
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    {insight.admin_email && <span>by {insight.admin_email}</span>}
                    {insight.created_at && (
                      <span>• {format(new Date(insight.created_at), 'MMM d, yyyy')}</span>
                    )}
                  </div>
                </div>
                {insight.tags && insight.tags.length > 0 && (
                  <div className="flex gap-1 flex-wrap">
                    {insight.tags.map((tag: string) => (
                      <Badge key={tag} variant="secondary" className="text-xs">
                        {tag}
                      </Badge>
                    ))}
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>
    )
  }

  return (
    <Card className="shadow-sm border-gray-200">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-xl font-semibold">Qualitative Insights</CardTitle>
            <CardDescription className="mt-1">Manual notes, complaints, and risk indicators</CardDescription>
          </div>
          <div className="h-10 w-10 rounded-lg bg-orange-100 flex items-center justify-center">
            <Flag className="h-5 w-5 text-orange-600" />
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="space-y-6">
          {renderInsightList(
            'Top Complaints',
            data.top_complaints || [],
            <AlertCircle className="h-4 w-4 text-red-500" />
          )}

          {renderInsightList(
            'Aha Moments',
            data.top_aha_moments || [],
            <Lightbulb className="h-4 w-4 text-yellow-500" />
          )}

          {renderInsightList(
            'Repeated Objections',
            data.repeated_objections || [],
            <MessageSquare className="h-4 w-4 text-blue-500" />
          )}

          {renderInsightList(
            'Risky Escalations',
            data.risky_escalations || [],
            <Flag className="h-4 w-4 text-orange-500" />
          )}

          {renderInsightList(
            'Onboarding Notes',
            data.onboarding_notes || [],
            <MessageSquare className="h-4 w-4 text-gray-500" />
          )}

          {(!data.top_complaints || data.top_complaints.length === 0) &&
            (!data.top_aha_moments || data.top_aha_moments.length === 0) &&
            (!data.repeated_objections || data.repeated_objections.length === 0) &&
            (!data.risky_escalations || data.risky_escalations.length === 0) &&
            (!data.onboarding_notes || data.onboarding_notes.length === 0) && (
              <p className="text-muted-foreground text-center py-8">
                No qualitative insights available
              </p>
            )}
        </div>
      </CardContent>
    </Card>
  )
}

