import { useState } from 'react'
import { Info, AlertCircle, TrendingUp, TrendingDown, ChevronDown, ChevronUp } from 'lucide-react'

interface DataInterpretationProps {
  title?: string
  children: React.ReactNode
  type?: 'info' | 'warning' | 'success' | 'neutral'
  className?: string
  defaultOpen?: boolean
}

export function DataInterpretation({ 
  title = 'Understanding This Data', 
  children, 
  type = 'info',
  className = '',
  defaultOpen = false
}: DataInterpretationProps) {
  const [isOpen, setIsOpen] = useState(defaultOpen)

  const typeStyles = {
    info: 'bg-blue-50 border-blue-200 text-blue-900',
    warning: 'bg-amber-50 border-amber-200 text-amber-900',
    success: 'bg-green-50 border-green-200 text-green-900',
    neutral: 'bg-gray-50 border-gray-200 text-gray-700'
  }

  const iconMap = {
    info: Info,
    warning: AlertCircle,
    success: TrendingUp,
    neutral: Info
  }

  const Icon = iconMap[type]

  return (
    <div className={`rounded-lg border ${typeStyles[type]} ${className} overflow-hidden`}>
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full flex items-center justify-between p-4 hover:bg-opacity-80 transition-colors"
      >
        <div className="flex items-center gap-3">
          <Icon className="h-5 w-5 flex-shrink-0" />
          <h4 className="font-semibold text-sm text-left">{title}</h4>
        </div>
        {isOpen ? (
          <ChevronUp className="h-4 w-4 flex-shrink-0" />
        ) : (
          <ChevronDown className="h-4 w-4 flex-shrink-0" />
        )}
      </button>
      {isOpen && (
        <div className="px-4 pb-4 text-sm space-y-1.5 leading-relaxed">
          {children}
        </div>
      )}
    </div>
  )
}

interface MetricExplanationProps {
  metric: string
  value: string | number
  explanation: string
  trend?: 'up' | 'down' | 'neutral'
  trendValue?: string
}

export function MetricExplanation({ 
  metric, 
  value, 
  explanation, 
  trend, 
  trendValue 
}: MetricExplanationProps) {
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <span className="font-medium text-gray-900">{metric}:</span>
        <div className="flex items-center gap-2">
          <span className="font-semibold text-gray-900">{value}</span>
          {trend === 'up' && trendValue && (
            <span className="flex items-center gap-1 text-sm text-green-600">
              <TrendingUp className="h-4 w-4" />
              {trendValue}
            </span>
          )}
          {trend === 'down' && trendValue && (
            <span className="flex items-center gap-1 text-sm text-red-600">
              <TrendingDown className="h-4 w-4" />
              {trendValue}
            </span>
          )}
        </div>
      </div>
      <p className="text-sm text-gray-600 leading-relaxed">{explanation}</p>
    </div>
  )
}

