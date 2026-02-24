import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { FileText, CheckCircle2, XCircle, Clock } from 'lucide-react'
import type { PostingStatusData } from '@/types/dashboard'

interface PostingStatusSectionProps {
  period: '24h' | '7d' | '30d' | 'custom'
  startDate?: string
  endDate?: string
}

export default function PostingStatusSection({ period, startDate, endDate }: PostingStatusSectionProps) {
  const { data, isLoading } = useQuery<PostingStatusData>({
    queryKey: ['posting-status', period, startDate, endDate],
    queryFn: () => api.getPostingStatus(period, startDate, endDate),
  })

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Posting Status</CardTitle>
          <CardDescription>Loading...</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (!data) {
    return (
      <Card className="shadow-sm border-gray-200">
        <CardHeader>
          <CardTitle className="text-xl font-semibold">Posting Status</CardTitle>
          <CardDescription>No data available</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  const successRate =
    data.posts_scheduled > 0
      ? ((data.posts_published / data.posts_scheduled) * 100).toFixed(1)
      : '0'

  return (
    <Card className="shadow-sm border-gray-200">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-xl font-semibold">Posting Status</CardTitle>
            <CardDescription className="mt-1">Post publishing success and failure tracking</CardDescription>
          </div>
          <div className="h-10 w-10 rounded-lg bg-gray-100 flex items-center justify-center">
            <FileText className="h-5 w-5 text-gray-600" />
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
          <div className="bg-gradient-to-br from-gray-50/50 to-transparent rounded-lg p-4 border border-gray-100">
            <div className="flex items-center gap-2 mb-2">
              <FileText className="h-4 w-4 text-gray-500" />
              <p className="text-xs font-medium text-gray-600">Scheduled</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">{data.posts_scheduled?.toLocaleString() || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-green-50/50 to-transparent rounded-lg p-4 border border-green-100">
            <div className="flex items-center gap-2 mb-2">
              <CheckCircle2 className="h-4 w-4 text-green-500" />
              <p className="text-xs font-medium text-gray-600">Published</p>
            </div>
            <p className="text-3xl font-bold text-green-600">
              {data.posts_published?.toLocaleString() || 0}
            </p>
          </div>

          <div className="bg-gradient-to-br from-red-50/50 to-transparent rounded-lg p-4 border border-red-100">
            <div className="flex items-center gap-2 mb-2">
              <XCircle className="h-4 w-4 text-red-500" />
              <p className="text-xs font-medium text-gray-600">Failed</p>
            </div>
            <p className="text-3xl font-bold text-red-600">{data.failed_posts || 0}</p>
          </div>

          <div className="bg-gradient-to-br from-blue-50/50 to-transparent rounded-lg p-4 border border-blue-100">
            <div className="flex items-center gap-2 mb-2">
              <Clock className="h-4 w-4 text-blue-500" />
              <p className="text-xs font-medium text-gray-600">Success Rate</p>
            </div>
            <p className="text-3xl font-bold text-gray-900">{successRate}%</p>
          </div>
        </div>

        {data.failure_reasons && data.failure_reasons.length > 0 && (
          <div className="border-t pt-4">
            <h4 className="font-semibold mb-3">Failure Reasons</h4>
            <div className="space-y-2">
              {data.failure_reasons.map((reason: any, idx: number) => (
                <div key={idx} className="flex items-center justify-between p-3 border rounded-lg">
                  <span className="text-sm">{reason.reason || 'Unknown error'}</span>
                  <Badge variant="destructive">{reason.count || 0}</Badge>
                </div>
              ))}
            </div>
          </div>
        )}

        {data.failed_posts === 0 && data.posts_published > 0 && (
          <div className="border-t pt-4">
            <p className="text-sm text-green-600 flex items-center gap-2">
              <CheckCircle2 className="h-4 w-4" />
              All posts published successfully
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

