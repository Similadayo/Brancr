import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export type DatePeriod = '24h' | '7d' | '30d' | 'custom'

interface DateRangeFilterProps {
    period: DatePeriod
    setPeriod: (period: DatePeriod) => void
    startDate: string
    setStartDate: (date: string) => void
    endDate: string
    setEndDate: (date: string) => void
    className?: string
}

export function DateRangeFilter({
    period,
    setPeriod,
    startDate,
    setStartDate,
    endDate,
    setEndDate,
    className = '',
}: DateRangeFilterProps) {
    return (
        <div className={`flex flex-col gap-2 ${className}`}>
            <div className="flex gap-2 flex-wrap items-center">
                <div className="flex gap-2 bg-white rounded-md border border-gray-200 p-1">
                    <Button
                        variant={period === '24h' ? 'secondary' : 'ghost'}
                        size="sm"
                        onClick={() => setPeriod('24h')}
                        className="text-xs"
                    >
                        Last 24h
                    </Button>
                    <Button
                        variant={period === '7d' ? 'secondary' : 'ghost'}
                        size="sm"
                        onClick={() => setPeriod('7d')}
                        className="text-xs"
                    >
                        Last 7 days
                    </Button>
                    <Button
                        variant={period === '30d' ? 'secondary' : 'ghost'}
                        size="sm"
                        onClick={() => setPeriod('30d')}
                        className="text-xs"
                    >
                        Last 30 days
                    </Button>
                    <Button
                        variant={period === 'custom' ? 'secondary' : 'ghost'}
                        size="sm"
                        onClick={() => setPeriod('custom')}
                        className="text-xs"
                    >
                        Custom
                    </Button>
                </div>
            </div>

            {period === 'custom' && (
                <div className="flex items-center gap-2 bg-white p-2 rounded-md border border-gray-200 animate-in fade-in slide-in-from-top-1">
                    <div className="flex items-center gap-2">
                        <Label htmlFor="start-date" className="text-xs whitespace-nowrap">From:</Label>
                        <Input
                            id="start-date"
                            type="date"
                            className="h-8 w-auto text-xs"
                            value={startDate}
                            onChange={(e) => setStartDate(e.target.value)}
                        />
                    </div>
                    <div className="flex items-center gap-2">
                        <Label htmlFor="end-date" className="text-xs whitespace-nowrap">To:</Label>
                        <Input
                            id="end-date"
                            type="date"
                            className="h-8 w-auto text-xs"
                            value={endDate}
                            onChange={(e) => setEndDate(e.target.value)}
                        />
                    </div>
                </div>
            )}
        </div>
    )
}
