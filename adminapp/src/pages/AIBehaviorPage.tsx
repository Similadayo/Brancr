import { useState } from 'react'
import AIBehaviorSection from '@/components/dashboard/AIBehaviorSection'
import { DateRangeFilter, DatePeriod } from '@/components/ui/date-range-filter'

export default function AIBehaviorPage() {
  const [period, setPeriod] = useState<DatePeriod>('24h')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')

  return (
    <div>
      <div className="mb-6 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">AI Behavior</h1>
          <p className="text-muted-foreground mt-1">AI performance metrics and trust indicators</p>
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
      <AIBehaviorSection period={period} startDate={startDate} endDate={endDate} />
    </div>
  )
}

