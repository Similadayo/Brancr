import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { CheckCircle2, XCircle, AlertCircle } from 'lucide-react'

interface HealthResponse {
  DB?: string
  Redis?: string
  Telegram?: string
  Cloudinary?: string
  OpenRouter?: string
  Notes?: Record<string, string>
  Timestamp?: string
}

export default function HealthPage() {
  const { data, isLoading } = useQuery<HealthResponse>({
    queryKey: ['health'],
    queryFn: () => api.getHealth(),
    refetchInterval: 30000, // Refetch every 30 seconds
  })

  if (isLoading) {
    return <div className="p-6">Loading health status...</div>
  }

  const healthChecks = [
    { name: 'Database', key: 'DB' },
    { name: 'Redis', key: 'Redis' },
    { name: 'Telegram', key: 'Telegram' },
    { name: 'Cloudinary', key: 'Cloudinary' },
    { name: 'OpenRouter', key: 'OpenRouter' },
  ]

  const getStatusBadge = (status: string) => {
    if (status === 'ok') {
      return (
        <Badge variant="default" className="flex items-center gap-1">
          <CheckCircle2 className="h-3 w-3" />
          Operational
        </Badge>
      )
    }
    return (
      <Badge variant="destructive" className="flex items-center gap-1">
        <XCircle className="h-3 w-3" />
        {status}
      </Badge>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">System Health</h1>
        <p className="text-muted-foreground">
          Monitor system components and services
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {healthChecks.map((check) => {
          const status = data?.[check.key as keyof typeof data] as string
          return (
            <Card key={check.key}>
              <CardHeader>
                <CardTitle className="text-lg">{check.name}</CardTitle>
              </CardHeader>
              <CardContent>
                {getStatusBadge(status || 'unknown')}
              </CardContent>
            </Card>
          )
        })}
      </div>

      {data?.Notes && Object.keys(data.Notes).length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Notes</CardTitle>
            <CardDescription>Additional information about system status</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {Object.entries(data.Notes).map(([key, value]) => (
                <div key={key} className="flex items-start gap-2">
                  <AlertCircle className="h-4 w-4 text-yellow-600 mt-0.5" />
                  <div>
                    <span className="font-medium">{key}:</span>{' '}
                    <span className="text-muted-foreground">{value as string}</span>
                  </div>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

