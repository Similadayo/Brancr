import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { MessageSquare, TrendingUp } from 'lucide-react'
import type { MessagingActivityData } from '@/types/dashboard'

interface MessagingActivitySectionProps {
  period: '24h' | '7d' | '30d' | 'custom'
  startDate?: string
  endDate?: string
}

export default function MessagingActivitySection({ period, startDate, endDate }: MessagingActivitySectionProps) {
  const { data, isLoading } = useQuery<MessagingActivityData>({
    queryKey: ['messaging-activity', period, startDate, endDate],
    queryFn: () => api.getMessagingActivity(period, startDate, endDate),
  })

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Messaging Activity</CardTitle>
          <CardDescription>Loading...</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (!data) {
    return (
      <Card className="shadow-sm border-gray-200">
        <CardHeader>
          <CardTitle className="text-xl font-semibold">Messaging Activity</CardTitle>
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
            <CardTitle className="text-xl font-semibold">Messaging Activity</CardTitle>
            <CardDescription className="mt-1">Core value metrics — conversations and messages</CardDescription>
          </div>
          <div className="h-10 w-10 rounded-lg bg-purple-100 flex items-center justify-center">
            <MessageSquare className="h-5 w-5 text-purple-600" />
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
          <div className="bg-gradient-to-br from-blue-50/50 to-transparent rounded-lg p-4 border border-blue-100">
            <div className="flex items-center gap-2 mb-2">
              <MessageSquare className="h-4 w-4 text-blue-500" />
              <p className="text-xs font-medium text-gray-600">Total Conversations</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">{data.total_conversations?.toLocaleString() || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-green-50/50 to-transparent rounded-lg p-4 border border-green-100">
            <p className="text-xs font-medium text-gray-600 mb-2">Messages by AI</p>
            <p className="text-3xl font-bold text-gray-900">{data.messages_by_ai?.toLocaleString() || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-yellow-50/50 to-transparent rounded-lg p-4 border border-yellow-100">
            <p className="text-xs font-medium text-gray-600 mb-2">Messages Escalated</p>
            <p className="text-3xl font-bold text-yellow-600">{data.messages_escalated || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-emerald-50/50 to-transparent rounded-lg p-4 border border-emerald-100">
            <div className="flex items-center gap-2 mb-2">
              <TrendingUp className="h-4 w-4 text-green-500" />
              <p className="text-xs font-medium text-gray-600">AI Automation Rate</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">
              {data.total_conversations > 0
                ? ((data.messages_by_ai / data.total_conversations) * 100).toFixed(1)
                : '0'}%
            </p>
          </div>
        </div>

        {data.messages_per_platform && data.messages_per_platform.length > 0 && (
          <div className="border-t border-gray-200 pt-4 mb-4">
            <h4 className="font-semibold text-base mb-3 text-gray-900">Messages by Platform</h4>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              {data.messages_per_platform.map((platform: any) => (
                <div key={platform.platform} className="p-3 border border-gray-200 rounded-lg bg-gray-50/50 hover:bg-gray-50 transition-colors">
                  <p className="text-xs font-medium text-gray-600 capitalize mb-1">{platform.platform}</p>
                  <p className="text-2xl font-bold text-gray-900">{platform.count?.toLocaleString() || 0}</p>
                </div>
              ))}
            </div>
          </div>
        )}

        {data.peak_hours && data.peak_hours.length > 0 && (
          <div className="border-t border-gray-200 pt-4">
            <h4 className="font-semibold text-base mb-3 text-gray-900">Peak Hours</h4>
            <div className="grid grid-cols-6 md:grid-cols-12 gap-2">
              {data.peak_hours.map((hour: any) => {
                const maxCount = Math.max(...data.peak_hours.map((h: any) => h.count || 0), 1)
                return (
                  <div key={hour.hour} className="text-center">
                    <p className="text-xs text-gray-500 font-medium mb-2">{hour.hour}:00</p>
                    <div className="h-20 bg-gray-100 rounded-md flex items-end justify-center p-1 hover:bg-gray-200 transition-colors">
                      <div
                        className="w-full bg-gradient-to-t from-purple-500 to-purple-400 rounded-md transition-all hover:from-purple-600 hover:to-purple-500"
                        style={{
                          height: `${Math.max((hour.count / maxCount) * 100, 8)}%`,
                        }}
                        title={`${hour.count || 0} messages`}
                      />
                    </div>
                    <p className="text-xs mt-1 font-medium text-gray-700">{hour.count || 0}</p>
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

