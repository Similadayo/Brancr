import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { format } from 'date-fns'
import { CreditCard, CheckCircle2, XCircle, Clock } from 'lucide-react'

interface Payment {
  id: string
  tenant_id: string
  tenant_name: string
  amount: number
  currency: string
  status: string
  payment_method: string
  plan_type: string
  created_at: string
}

interface PaymentSummary {
  total_revenue: number
  this_month_revenue: number
  failed_payments: number
  pending_payments: number
  active_subscriptions: number
}

interface PaymentsResponse {
  total: number
  items: Payment[]
  summary: PaymentSummary
}

export default function PaymentsPage() {
  const { data, isLoading } = useQuery<PaymentsResponse>({
    queryKey: ['payments'],
    queryFn: () => api.getPayments(),
  })

  if (isLoading) {
    return <div className="p-6">Loading payments...</div>
  }

  const getStatusBadge = (status: string) => {
    const variants: Record<string, { variant: 'default' | 'secondary' | 'destructive' | 'outline'; icon: React.ReactNode }> = {
      completed: { variant: 'default', icon: <CheckCircle2 className="h-3 w-3" /> },
      pending: { variant: 'secondary', icon: <Clock className="h-3 w-3" /> },
      failed: { variant: 'destructive', icon: <XCircle className="h-3 w-3" /> },
      refunded: { variant: 'outline', icon: <CreditCard className="h-3 w-3" /> },
    }
    const config = variants[status] || variants.pending
    return (
      <Badge variant={config.variant} className="flex items-center gap-1">
        {config.icon}
        {status}
      </Badge>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Payments</h1>
        <p className="text-muted-foreground">
          Tenant-to-Brancr subscription payments ({data?.total || 0} total)
        </p>
      </div>

      {data?.summary && (
        <div className="grid gap-4 md:grid-cols-4">
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-sm font-medium">Total Revenue</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                ${(data.summary.total_revenue || 0).toFixed(2)}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-sm font-medium">This Month</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                ${(data.summary.this_month_revenue || 0).toFixed(2)}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-sm font-medium">Failed</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{data.summary.failed_payments || 0}</div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-sm font-medium">Active Subscriptions</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{data.summary.active_subscriptions || 0}</div>
            </CardContent>
          </Card>
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Payment History</CardTitle>
          <CardDescription>Recent tenant subscription payments</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {data?.items && data.items.length > 0 ? (
              data.items.map((payment: any) => (
                <div key={payment.id} className="flex items-center justify-between p-4 border rounded-lg">
                  <div>
                    <div className="font-medium">{payment.tenant_name}</div>
                    <div className="text-sm text-muted-foreground">
                      {payment.plan_type} • {format(new Date(payment.created_at), 'PPp')}
                    </div>
                  </div>
                  <div className="flex items-center gap-4">
                    <div className="text-right">
                      <div className="font-medium">
                        {payment.currency} {payment.amount?.toFixed(2)}
                      </div>
                    </div>
                    {getStatusBadge(payment.status)}
                  </div>
                </div>
              ))
            ) : (
              <div className="py-12 text-center text-muted-foreground">
                No payments found. Payment tracking will be available when tenant subscription system is implemented.
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

