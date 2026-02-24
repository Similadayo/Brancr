import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { format } from 'date-fns'
import { Users, Hash } from 'lucide-react'
import { DataInterpretation } from '@/components/ui/data-interpretation'
import { DateRangeFilter, DatePeriod } from '@/components/ui/date-range-filter'

interface AnalyticsResponse {
  total_posts: number
  total_engagement: number
  top_platforms: Array<{ platform: string; post_count: number; engagement: number }>
  top_tenants: Array<{ tenant_id: string; tenant_name: string; post_count: number; engagement: number }>
  recent_posts: Array<{
    id: string
    tenant_id: string
    tenant_name: string
    platform: string
    posted_at: string
    likes: number
    comments: number
    shares: number
  }>
}

export default function AnalyticsPage() {
  const [period, setPeriod] = useState<DatePeriod>('30d')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')

  const { data, isLoading } = useQuery<AnalyticsResponse>({
    queryKey: ['analytics', period, startDate, endDate],
    queryFn: () => api.getAnalytics(period, startDate, endDate),
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600 mx-auto mb-4"></div>
          <p className="text-gray-600">Loading analytics...</p>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-gray-900">Analytics</h1>
          <p className="text-sm sm:text-base text-gray-600 mt-1">
            Platform and tenant analytics dashboard
          </p>
        </div>
        <div className="flex gap-2">
          <DateRangeFilter
            period={period}
            setPeriod={setPeriod}
            startDate={startDate}
            setStartDate={setStartDate}
            endDate={endDate}
            setEndDate={setEndDate}
          />
        </div>
      </div>

      <DataInterpretation
        title="Understanding Analytics Metrics"
        type="info"
      >
        <p className="mb-3">
          Analytics provides aggregated insights into post performance across platforms and tenants. This helps identify top-performing content, successful tenants, and platform engagement patterns to inform product strategy and tenant success initiatives.
        </p>
        <p className="mb-2 font-semibold">Key Metrics:</p>
        <ul className="list-disc list-inside space-y-2 ml-2 mb-3">
          <li><strong>Total Posts:</strong> Total number of posts published across all tenants and platforms during the analytics period (typically 30 days). This shows overall platform content volume and growth.</li>
          <li><strong>Total Engagement:</strong> Aggregate engagement across all posts (likes, comments, shares, reactions combined). This is a key indicator of content performance and audience engagement. Higher engagement indicates content is resonating with audiences.</li>
          <li><strong>Top Platforms:</strong> Platforms ranked by post count and engagement. This shows which social media platforms are most popular with tenants and generate the most engagement. Use this to prioritize platform feature development and optimization.</li>
          <li><strong>Top Tenants:</strong> Tenants ranked by engagement metrics. These are the most successful tenants in terms of content performance. Identify what makes them successful (content quality, posting frequency, platform selection) and share best practices with other tenants.</li>
          <li><strong>Recent Posts:</strong> Latest posts with their engagement metrics (likes, comments, shares). This provides real-time visibility into content performance and helps identify trending topics or successful content types.</li>
        </ul>
        <p className="mb-2 font-semibold">Engagement Metrics Explained:</p>
        <ul className="list-disc list-inside space-y-1 ml-2 mb-3">
          <li><strong>Likes:</strong> Positive reactions to posts (Instagram likes, Facebook likes/reactions, TikTok likes). High like counts indicate content is well-received.</li>
          <li><strong>Comments:</strong> User comments on posts. Comments indicate deeper engagement and conversation around content. High comment counts often correlate with viral content or controversial topics.</li>
          <li><strong>Shares:</strong> Posts that were shared/reposted by users. Shares are a strong indicator of content value, as users only share content they find valuable or want to share with their network.</li>
          <li><strong>Engagement Rate:</strong> Calculated as `(likes + comments + shares) / reach`. Higher engagement rates indicate content is resonating with audiences relative to the number of people who saw it.</li>
        </ul>
        <p className="mb-2">
          <strong>Action Items:</strong>
        </p>
        <ul className="list-disc list-inside space-y-1 ml-4 mb-3">
          <li>Identify top-performing tenants and analyze their content strategies, posting frequency, and platform selection to develop success playbooks.</li>
          <li>Use top platforms data to prioritize platform-specific feature development and optimization efforts.</li>
          <li>Monitor recent posts to identify trending content types, topics, or formats that generate high engagement.</li>
          <li>Compare engagement rates across platforms to help tenants optimize their platform mix and content strategy.</li>
          <li>Share insights from top tenants with other tenants through newsletters, webinars, or one-on-one consultations to improve overall platform performance.</li>
        </ul>
        <p>
          <strong>Data Source:</strong> Analytics are calculated from the `post_analytics` table, which aggregates engagement metrics from platform APIs. Data is collected after posts are published and engagement metrics are fetched from platform APIs (Instagram Graph API, Facebook Graph API, TikTok API, etc.). The analytics period defaults to 30 days but can be adjusted. Engagement data may have a delay depending on platform API refresh rates.
        </p>
      </DataInterpretation>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium">Total Posts</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{data?.total_posts || 0}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium">Total Engagement</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{data?.total_engagement || 0}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium">Top Platforms</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{data?.top_platforms?.length || 0}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium">Active Tenants</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{data?.top_tenants?.length || 0}</div>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Top Platforms</CardTitle>
            <CardDescription>By post count and engagement</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              {data?.top_platforms?.slice(0, 5).map((platform: any) => (
                <div key={platform.platform} className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Hash className="h-4 w-4 text-muted-foreground" />
                    <span className="font-medium">{platform.platform}</span>
                  </div>
                  <div className="flex items-center gap-4">
                    <Badge variant="outline">{platform.post_count} posts</Badge>
                    <Badge>{platform.engagement} engagement</Badge>
                  </div>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Top Tenants</CardTitle>
            <CardDescription>By engagement</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              {data?.top_tenants?.slice(0, 5).map((tenant: any) => (
                <div key={tenant.tenant_id} className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Users className="h-4 w-4 text-muted-foreground" />
                    <span className="font-medium">{tenant.tenant_name}</span>
                  </div>
                  <div className="flex items-center gap-4">
                    <Badge variant="outline">{tenant.post_count} posts</Badge>
                    <Badge>{tenant.engagement} engagement</Badge>
                  </div>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent Posts</CardTitle>
          <CardDescription>Latest post analytics</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            {data?.recent_posts?.slice(0, 10).map((post: any) => (
              <div key={post.id} className="flex items-center justify-between p-3 border rounded-lg">
                <div>
                  <div className="font-medium">{post.tenant_name}</div>
                  <div className="text-sm text-muted-foreground">
                    {post.platform} • {format(new Date(post.posted_at), 'PPp')}
                  </div>
                </div>
                <div className="flex items-center gap-4">
                  <Badge variant="outline">👍 {post.likes}</Badge>
                  <Badge variant="outline">💬 {post.comments}</Badge>
                  <Badge variant="outline">🔁 {post.shares}</Badge>
                </div>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

