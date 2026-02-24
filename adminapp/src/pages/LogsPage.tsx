import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { format } from 'date-fns'
import { Pause, Play, Trash2, Activity, Terminal } from 'lucide-react'
import { useState, useEffect, useRef } from 'react'
import { DateRangeFilter, DatePeriod } from '@/components/ui/date-range-filter'

interface LogEntry {
  id: string
  level: string
  message: string
  source?: string
  tenant_id?: string
  created_at: string
}

interface LogsResponse {
  total: number
  items: LogEntry[]
}

// Stream Log Entry format might differ slightly from DB entry
interface StreamLogEntry {
  level: string
  ts: string // ISO timestamp
  caller: string
  msg: string
  request_id?: string
}

export default function LogsPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">System Logs</h1>
          <p className="text-muted-foreground">
            Monitor real-time system events and review historical logs
          </p>
        </div>
      </div>

      <Tabs defaultValue="live" className="space-y-4">
        <TabsList>
          <TabsTrigger value="live" className="flex items-center gap-2">
            <Activity className="h-4 w-4" />
            Live Stream
          </TabsTrigger>
          <TabsTrigger value="history" className="flex items-center gap-2">
            <Terminal className="h-4 w-4" />
            Audit History
          </TabsTrigger>
        </TabsList>

        <TabsContent value="live" className="space-y-4">
          <LiveLogStream />
        </TabsContent>

        <TabsContent value="history" className="space-y-4">
          <HistoricalLogs />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function LiveLogStream() {
  const [logs, setLogs] = useState<StreamLogEntry[]>([])
  const [isConnected, setIsConnected] = useState(false)
  const [isPaused, setIsPaused] = useState(false)
  const [autoScroll, setAutoScroll] = useState(true)
  const scrollRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)

  // Maximum logs to keep in memory
  const MAX_LOGS = 1000

  useEffect(() => {
    connect()
    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
      }
    }
  }, [])

  useEffect(() => {
    if (autoScroll && scrollRef.current && !isPaused) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [logs, autoScroll, isPaused])

  const connect = () => {
    // Use SSE (Server-Sent Events) instead of WebSocket for better proxy compatibility
    const port = window.location.port ? `:${window.location.port}` : ''
    const url = `${window.location.protocol}//${window.location.hostname}${port}/admin/api/logs/stream`

    console.log('Connecting to SSE:', url)

    const eventSource = new EventSource(url, { withCredentials: true })
    eventSourceRef.current = eventSource

    eventSource.onopen = () => {
      console.log('SSE Connected')
      setIsConnected(true)
    }

    eventSource.onerror = (err) => {
      console.error('SSE Error:', err)
      setIsConnected(false)
      eventSource.close()
      // Simple reconnect logic
      setTimeout(connect, 3000)
    }

    eventSource.onmessage = (event) => {
      if (isPaused) return

      try {
        const data = JSON.parse(event.data)
        setLogs(prev => {
          const newLogs = [...prev, data]
          if (newLogs.length > MAX_LOGS) {
            return newLogs.slice(newLogs.length - MAX_LOGS)
          }
          return newLogs
        })
      } catch (e) {
        console.error('Failed to parse log message:', event.data)
      }
    }
  }

  const clearLogs = () => setLogs([])
  const togglePause = () => setIsPaused(!isPaused)

  return (
    <Card className="flex flex-col h-[calc(100vh-250px)] min-h-[500px]">
      <CardHeader className="flex flex-row items-center justify-between py-2 border-b">
        <div className="flex items-center gap-2">
          <CardTitle className="text-sm font-medium">Console Output</CardTitle>
          <Badge variant={isConnected ? "default" : "destructive"} className="text-xs">
            {isConnected ? 'Connected' : 'Disconnected'}
          </Badge>
          <Badge variant="outline" className="text-xs">
            {logs.length} lines
          </Badge>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={togglePause}>
            {isPaused ? <Play className="h-4 w-4 mr-1" /> : <Pause className="h-4 w-4 mr-1" />}
            {isPaused ? 'Resume' : 'Pause'}
          </Button>
          <Button variant="ghost" size="sm" onClick={clearLogs}>
            <Trash2 className="h-4 w-4 mr-1" />
            Clear
          </Button>
        </div>
      </CardHeader>
      <div
        ref={scrollRef}
        className="flex-1 overflow-auto bg-black p-4 font-mono text-xs text-white"
        onScroll={(e) => {
          const target = e.target as HTMLDivElement
          // Disable autoscroll if user scrolls up
          const isAtBottom = Math.abs(target.scrollHeight - target.scrollTop - target.clientHeight) < 10
          setAutoScroll(isAtBottom)
        }}
      >
        {logs.length === 0 && isConnected && (
          <div className="text-gray-500 italic">Waiting for logs...</div>
        )}
        {logs.map((log, i) => (
          <div key={i} className="flex gap-2 hover:bg-gray-900 border-l-2 border-transparent hover:border-blue-500 pl-1">
            <span className="text-gray-500 shrink-0 select-none">
              {format(new Date(log.ts), 'HH:mm:ss.SSS')}
            </span>
            <span className={`font-bold shrink-0 w-16 ${getLevelColor(log.level)}`}>
              {log.level.toUpperCase()}
            </span>
            <span className="text-gray-400 shrink-0">
              [{log.caller}]
            </span>
            <span className="text-gray-100 break-all whitespace-pre-wrap">
              {log.msg}
            </span>
            {log.request_id && (
              <span className="text-gray-600 text-[10px] ml-auto">
                {log.request_id}
              </span>
            )}
          </div>
        ))}
        {!isConnected && (
          <div className="text-red-500 mt-2">Connection lost. Reconnecting...</div>
        )}
      </div>
    </Card>
  )
}

function HistoricalLogs() {
  const [period, setPeriod] = useState<DatePeriod>('24h')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')

  const { data, isLoading } = useQuery<LogsResponse>({
    queryKey: ['logs', period, startDate, endDate],
    queryFn: () => api.getLogs(undefined, undefined, undefined, startDate, endDate),
  })

  if (isLoading) {
    return <div className="p-6">Loading logs...</div>
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <div>
          <CardTitle>Audit & Error Logs</CardTitle>
          <CardDescription>
            {data?.total || 0} entries found
          </CardDescription>
        </div>
        <DateRangeFilter
          period={period}
          setPeriod={setPeriod}
          startDate={startDate}
          setStartDate={setStartDate}
          endDate={endDate}
          setEndDate={setEndDate}
        />
      </CardHeader>
      <CardContent className="p-0">
        <div className="divide-y relative">
          <div className="sticky top-0 bg-secondary/50 p-2 grid grid-cols-12 text-xs font-medium text-muted-foreground">
            <div className="col-span-1">LEVEL</div>
            <div className="col-span-2">TIMESTAMP</div>
            <div className="col-span-2">SOURCE</div>
            <div className="col-span-7">MESSAGE</div>
          </div>
          {data?.items?.map((log: any) => (
            <div key={log.id} className="p-2 grid grid-cols-12 items-start gap-2 text-sm hover:bg-muted/50">
              <div className="col-span-1">
                {getLevelBadge(log.level)}
              </div>
              <div className="col-span-2 text-muted-foreground text-xs pt-1">
                {(() => {
                  try {
                    if (!log.created_at) return 'No date'
                    const date = new Date(log.created_at)
                    if (isNaN(date.getTime())) return 'Invalid date'
                    return format(date, 'PPp')
                  } catch {
                    return 'Invalid date'
                  }
                })()}
              </div>
              <div className="col-span-2">
                {log.source && (
                  <Badge variant="outline" className="text-xs">{log.source}</Badge>
                )}
              </div>
              <div className="col-span-7 break-words">
                {log.message}
              </div>
            </div>
          ))}
        </div>

        {(!data?.items || data.items.length === 0) && (
          <div className="py-12 text-center text-muted-foreground">
            No logs found
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function getLevelBadge(level: string) {
  switch (level.toLowerCase()) {
    case 'error':
      return <Badge variant="destructive">Error</Badge>
    case 'warn':
    case 'warning':
      return <Badge variant="secondary" className="bg-yellow-100 text-yellow-800 hover:bg-yellow-100">Warning</Badge>
    case 'info':
      return <Badge variant="secondary" className="bg-blue-100 text-blue-800 hover:bg-blue-100">Info</Badge>
    default:
      return <Badge variant="outline">{level}</Badge>
  }
}

function getLevelColor(level: string) {
  switch (level.toLowerCase()) {
    case 'error':
      return 'text-red-500'
    case 'warn':
    case 'warning':
      return 'text-yellow-500'
    case 'info':
      return 'text-blue-400'
    case 'debug':
      return 'text-gray-500'
    default:
      return 'text-white'
  }
}
