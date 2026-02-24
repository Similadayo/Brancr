import { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

interface StatCardProps {
  title: string
  value: string | number
  change?: string
  icon: LucideIcon
  iconColor?: string
  gradient?: string
  trend?: 'up' | 'down' | 'neutral'
}

export default function StatCard({
  title,
  value,
  change,
  icon: Icon,
  iconColor = 'text-blue-500',
  gradient = 'from-blue-500/10 to-blue-600/10',
  trend,
}: StatCardProps) {
  const trendColor =
    trend === 'up'
      ? 'text-green-600'
      : trend === 'down'
      ? 'text-red-600'
      : 'text-gray-600'

  return (
    <div className="relative overflow-hidden rounded-xl border bg-white p-6 shadow-sm transition-all hover:shadow-md">
      {/* Gradient background */}
      <div className={cn('absolute inset-0 bg-gradient-to-br', gradient)} />
      
      <div className="relative">
        <div className="flex items-center justify-between">
          <div className="flex-1">
            <p className="text-sm font-medium text-gray-600">{title}</p>
            <div className="mt-2 flex items-baseline gap-2">
              <p className="text-3xl font-bold text-gray-900">
                {typeof value === 'number' ? value.toLocaleString() : value}
              </p>
              {change && (
                <span className={cn('text-sm font-medium', trendColor)}>
                  {change}
                </span>
              )}
            </div>
          </div>
          <div
            className={cn(
              'flex h-12 w-12 items-center justify-center rounded-lg',
              iconColor.replace('text-', 'bg-').replace('-500', '-100')
            )}
          >
            <Icon className={cn('h-6 w-6', iconColor)} />
          </div>
        </div>
      </div>
    </div>
  )
}

