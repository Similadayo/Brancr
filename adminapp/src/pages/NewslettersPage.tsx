import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { format } from 'date-fns'
import { Send, Plus, CheckCircle2, XCircle, Clock } from 'lucide-react'
import { useState } from 'react'

interface Newsletter {
  id: string
  title: string
  subject: string
  status: 'draft' | 'scheduled' | 'sending' | 'sent' | 'failed'
  scheduled_at?: string
  sent_at?: string
  total_recipients: number
  sent_count: number
  delivered_count: number
  opened_count: number
  clicked_count: number
  failed_count: number
  created_at: string
  created_by: string
}

export default function NewslettersPage() {
  const [showCreateForm, setShowCreateForm] = useState(false)
  const [formData, setFormData] = useState({
    title: '',
    subject: '',
    content: '',
    categories: [] as string[],
    scheduled_at: '',
  })
  const queryClient = useQueryClient()

  const { data: newsletters } = useQuery({
    queryKey: ['newsletters'],
    queryFn: () => api.getNewsletters(),
  })

  const { data: subscriptions } = useQuery({
    queryKey: ['newsletter-subscriptions'],
    queryFn: () => api.getNewsletterSubscriptions(),
  })

  const createMutation = useMutation({
    mutationFn: (data: typeof formData) => api.createNewsletter(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['newsletters'] })
      setShowCreateForm(false)
      setFormData({
        title: '',
        subject: '',
        content: '',
        categories: [],
        scheduled_at: '',
      })
    },
  })

  const sendMutation = useMutation({
    mutationFn: (id: string) => api.sendNewsletter(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['newsletters'] })
    },
  })

  const getStatusBadge = (status: string) => {
    const variants: Record<string, { variant: 'default' | 'secondary' | 'destructive' | 'outline'; icon: React.ReactNode }> = {
      draft: { variant: 'outline', icon: <Clock className="h-3 w-3" /> },
      scheduled: { variant: 'secondary', icon: <Clock className="h-3 w-3" /> },
      sending: { variant: 'default', icon: <Send className="h-3 w-3" /> },
      sent: { variant: 'default', icon: <CheckCircle2 className="h-3 w-3" /> },
      failed: { variant: 'destructive', icon: <XCircle className="h-3 w-3" /> },
    }
    const config = variants[status] || variants.draft
    return (
      <Badge variant={config.variant} className="flex items-center gap-1">
        {config.icon}
        {status}
      </Badge>
    )
  }

  const subscribedCount = subscriptions?.filter((s: any) => s.subscribed).length || 0

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Newsletters</h1>
          <p className="text-muted-foreground">
            Create and send newsletters to subscribed tenants ({subscribedCount} subscribers)
          </p>
        </div>
        <Button onClick={() => setShowCreateForm(!showCreateForm)}>
          <Plus className="h-4 w-4 mr-2" />
          Create Newsletter
        </Button>
      </div>

      {showCreateForm && (
        <Card>
          <CardHeader>
            <CardTitle>Create Newsletter</CardTitle>
            <CardDescription>Compose a newsletter to send to all subscribed tenants</CardDescription>
          </CardHeader>
          <CardContent>
            <form
              onSubmit={(e) => {
                e.preventDefault()
                createMutation.mutate(formData)
              }}
              className="space-y-4"
            >
              <div>
                <label className="text-sm font-medium mb-2 block">Title *</label>
                <input
                  type="text"
                  className="w-full p-2 border rounded"
                  value={formData.title}
                  onChange={(e) => setFormData({ ...formData, title: e.target.value })}
                  required
                />
              </div>

              <div>
                <label className="text-sm font-medium mb-2 block">Email Subject *</label>
                <input
                  type="text"
                  className="w-full p-2 border rounded"
                  value={formData.subject}
                  onChange={(e) => setFormData({ ...formData, subject: e.target.value })}
                  required
                />
              </div>

              <div>
                <label className="text-sm font-medium mb-2 block">Content (HTML) *</label>
                <textarea
                  className="w-full min-h-[300px] p-2 border rounded font-mono text-sm"
                  value={formData.content}
                  onChange={(e) => setFormData({ ...formData, content: e.target.value })}
                  required
                  placeholder="Enter HTML content for the newsletter..."
                />
              </div>

              <div>
                <label className="text-sm font-medium mb-2 block">Scheduled At (optional, ISO 8601)</label>
                <input
                  type="datetime-local"
                  className="w-full p-2 border rounded"
                  value={formData.scheduled_at}
                  onChange={(e) => setFormData({ ...formData, scheduled_at: e.target.value })}
                />
              </div>

              <div className="flex gap-2">
                <Button type="submit" disabled={createMutation.isPending}>
                  {createMutation.isPending ? 'Creating...' : 'Create Newsletter'}
                </Button>
                <Button type="button" variant="outline" onClick={() => setShowCreateForm(false)}>
                  Cancel
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-4">
        {newsletters?.map((newsletter: Newsletter) => (
          <Card key={newsletter.id}>
            <CardHeader>
              <div className="flex items-start justify-between">
                <div>
                  <CardTitle className="text-lg">{newsletter.title}</CardTitle>
                  <CardDescription>
                    Subject: {newsletter.subject} • Created by {newsletter.created_by} •{' '}
                    {format(new Date(newsletter.created_at), 'PPp')}
                  </CardDescription>
                </div>
                {getStatusBadge(newsletter.status)}
              </div>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-4">
                <div>
                  <div className="text-sm text-muted-foreground">Recipients</div>
                  <div className="text-2xl font-bold">{newsletter.total_recipients}</div>
                </div>
                <div>
                  <div className="text-sm text-muted-foreground">Sent</div>
                  <div className="text-2xl font-bold">{newsletter.sent_count}</div>
                </div>
                <div>
                  <div className="text-sm text-muted-foreground">Opened</div>
                  <div className="text-2xl font-bold">{newsletter.opened_count}</div>
                </div>
                <div>
                  <div className="text-sm text-muted-foreground">Clicked</div>
                  <div className="text-2xl font-bold">{newsletter.clicked_count}</div>
                </div>
              </div>

              {newsletter.status === 'draft' && (
                <Button
                  size="sm"
                  onClick={() => sendMutation.mutate(newsletter.id)}
                  disabled={sendMutation.isPending}
                >
                  <Send className="h-4 w-4 mr-1" />
                  Send Now
                </Button>
              )}
            </CardContent>
          </Card>
        ))}

        {(!newsletters || newsletters.length === 0) && (
          <Card>
            <CardContent className="py-12 text-center text-muted-foreground">
              No newsletters created yet
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}

