import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { format } from 'date-fns'
import { AlertCircle, CheckCircle2, Clock, XCircle, X } from 'lucide-react'
import { useState } from 'react'

interface ScheduledPost {
  id: string
  tenant_id: string
  tenant_name: string
  name: string
  caption: string
  platforms: string[]
  scheduled_at: string | null
  status: 'scheduled' | 'posting' | 'posted' | 'failed' | 'cancelled'
  attempts: number
  last_error?: string
  posted_at?: string
  created_at: string
}

interface ScheduledPostsResponse {
  total: number
  items: ScheduledPost[]
}

export default function ScheduledPostsPage() {
  const [cancelPostId, setCancelPostId] = useState<string | null>(null)
  const [cancelReason, setCancelReason] = useState('')
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery<ScheduledPostsResponse>({
    queryKey: ['scheduled-posts'],
    queryFn: () => api.getScheduledPosts(),
  })

  const cancelMutation = useMutation({
    mutationFn: ({ id, reason }: { id: string; reason: string }) =>
      api.cancelScheduledPost(id, reason),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['scheduled-posts'] })
      setCancelPostId(null)
      setCancelReason('')
    },
  })

  const getStatusBadge = (status: string) => {
    const variants: Record<string, { variant: 'default' | 'secondary' | 'destructive' | 'outline'; icon: React.ReactNode }> = {
      scheduled: { variant: 'default', icon: <Clock className="h-3 w-3" /> },
      posting: { variant: 'secondary', icon: <Clock className="h-3 w-3" /> },
      posted: { variant: 'default', icon: <CheckCircle2 className="h-3 w-3" /> },
      failed: { variant: 'destructive', icon: <AlertCircle className="h-3 w-3" /> },
      cancelled: { variant: 'outline', icon: <XCircle className="h-3 w-3" /> },
    }
    const config = variants[status] || variants.scheduled
    return (
      <Badge variant={config.variant} className="flex items-center gap-1">
        {config.icon}
        {status}
      </Badge>
    )
  }

  if (isLoading) {
    return <div className="p-6">Loading scheduled posts...</div>
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Scheduled Posts</h1>
        <p className="text-muted-foreground">
          Manage and monitor scheduled social media posts ({data?.total || 0} total)
        </p>
      </div>

      <div className="grid gap-4">
        {data?.items?.map((post: ScheduledPost) => (
          <Card key={post.id}>
            <CardHeader>
              <div className="flex items-start justify-between">
                <div>
                  <CardTitle className="text-lg">{post.name || `Post #${post.id}`}</CardTitle>
                  <CardDescription>
                    Tenant: {post.tenant_name} • {(() => {
                      try {
                        if (!post.created_at) return 'No date'
                        const date = new Date(post.created_at)
                        if (isNaN(date.getTime())) return 'Invalid date'
                        return format(date, 'PPp')
                      } catch {
                        return 'Invalid date'
                      }
                    })()}
                  </CardDescription>
                </div>
                {getStatusBadge(post.status)}
              </div>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                {post.scheduled_at && (
                  <div className="text-sm">
                    <span className="font-medium">Scheduled for: </span>
                    {(() => {
                      try {
                        const date = new Date(post.scheduled_at)
                        if (isNaN(date.getTime())) return 'Invalid date'
                        return format(date, 'PPp')
                      } catch {
                        return 'Invalid date'
                      }
                    })()}
                  </div>
                )}

                <div className="flex flex-wrap gap-2">
                  {post.platforms?.map((platform) => (
                    <Badge key={platform} variant="outline">
                      {platform}
                    </Badge>
                  ))}
                </div>

                {post.caption && (
                  <p className="text-sm text-muted-foreground line-clamp-2">{post.caption}</p>
                )}

                {post.last_error && (
                  <div className="text-sm text-destructive bg-destructive/10 p-2 rounded">
                    {post.last_error}
                  </div>
                )}

                {post.status === 'scheduled' && (
                  <div className="flex gap-2">
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => setCancelPostId(post.id)}
                    >
                      <X className="h-4 w-4 mr-1" />
                      Cancel
                    </Button>
                  </div>
                )}

                {cancelPostId === post.id && (
                  <div className="border rounded-lg p-4 bg-muted/50 space-y-3">
                    <div>
                      <label className="text-sm font-medium mb-2 block">Cancellation Reason</label>
                      <textarea
                        className="w-full min-h-[80px] p-2 border rounded"
                        value={cancelReason}
                        onChange={(e) => setCancelReason(e.target.value)}
                        placeholder="Enter reason for cancellation..."
                      />
                    </div>
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        onClick={() => {
                          cancelMutation.mutate({ id: post.id, reason: cancelReason || 'Cancelled by admin' })
                        }}
                        disabled={cancelMutation.isPending}
                      >
                        Confirm Cancel
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                          setCancelPostId(null)
                          setCancelReason('')
                        }}
                      >
                        Cancel
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            </CardContent>
          </Card>
        ))}

        {(!data?.items || data.items.length === 0) && (
          <Card>
            <CardContent className="py-12 text-center text-muted-foreground">
              No scheduled posts found
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}

