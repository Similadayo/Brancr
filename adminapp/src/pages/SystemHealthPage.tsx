import { useState } from 'react'
import SystemHealthSection from '@/components/dashboard/SystemHealthSection'
import { DataInterpretation } from '@/components/ui/data-interpretation'
import { DateRangeFilter, DatePeriod } from '@/components/ui/date-range-filter'

export default function SystemHealthPage() {
  const [period, setPeriod] = useState<DatePeriod>('24h')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold text-gray-900">System Health</h1>
          <p className="text-sm sm:text-base text-gray-600 mt-1">Platform status and system metrics</p>
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

      {/* Data Interpretation */}
      <DataInterpretation
        title="Understanding System Health Metrics"
        type="info"
      >
        <p className="mb-3">
          System Health provides real-time visibility into the operational status of all platform integrations and core system components. All metrics shown here are calculated from actual database records and log entries from the selected time period.
        </p>
        <p className="mb-2 font-semibold">Platform Health Metrics (per platform):</p>
        <ul className="list-disc list-inside space-y-2 ml-2 mb-3">
          <li><strong>Received:</strong> Total number of inbound messages received from customers on this platform during the period. Counted from the `interactions` table where `direction = "inbound"`. A value of 0 means no messages were received, which could indicate either no customer activity or connectivity issues if you expect messages.</li>
          <li><strong>Sent:</strong> Total number of outbound messages sent to customers on this platform. Counted from `interactions` where `direction = "outbound"`. This includes both AI-generated replies and manual responses sent by tenant staff.</li>
          <li><strong>Failed:</strong> Number of outbound messages that failed to send. Counted from interactions with `response_status IN ("failed", "error")` or from error log entries. High failure counts indicate platform API issues, authentication problems, or network connectivity problems that need immediate investigation.</li>
          <li><strong>Retries:</strong> Number of messages that were retried after an initial failure. Extracted from interaction metadata containing "retry" keywords. High retry counts suggest transient issues that are being automatically resolved, but persistent retries indicate systemic problems.</li>
          <li><strong>API Errors:</strong> Count of API-related errors logged for this platform. Counted from `log_entries` where the source contains the platform name and the message contains "api". These are errors returned by the platform's API (e.g., rate limits, authentication failures, invalid requests) and require platform-specific investigation.</li>
          <li><strong>Status:</strong> Overall health status (Green = Healthy, Amber = Warning, Red = Critical). Calculated as: Red if failed_sends &gt; 10 OR api_errors &gt; 5; Amber if failed_sends &gt; 5 OR api_errors &gt; 2; Green otherwise.</li>
        </ul>
        <p className="mb-2 font-semibold">System Metrics:</p>
        <ul className="list-disc list-inside space-y-2 ml-2 mb-3">
          <li><strong>Job Failures:</strong> Total number of background job failures during the period. Counted from log entries where `source IN ("scheduler", "worker", "background")` and `level = "ERROR"`. Background jobs include scheduled tasks like metric aggregation, scheduled posts, and periodic maintenance tasks. High failure counts indicate issues with the job processing system.</li>
          <li><strong>Queue Backlog:</strong> Number of pending operations waiting to be processed. Counted from interactions with `response_status = "pending"` from the last 24 hours, or from scheduled posts with `status = "pending"` that are due. A growing backlog indicates the system is unable to keep up with demand, which could point to resource constraints or processing bottlenecks.</li>
          <li><strong>Service Uptime:</strong> Calculated uptime percentage based on error rate from log entries over the last 7 days. Formula: `100 - (error_requests / total_requests * 100)`. Shows the percentage of successful operations vs. total operations. 100% means no errors recorded; lower percentages indicate service degradation. This is a rolling 7-day calculation, not real-time.</li>
          <li><strong>Last Incident:</strong> Timestamp of the most recent ERROR-level log entry in the system. This helps identify when the last system-wide issue occurred. If this timestamp is recent and you're seeing ongoing errors, it indicates an active incident that needs investigation.</li>
        </ul>
        <p className="mb-2">
          <strong>Action Items:</strong>
        </p>
        <ul className="list-disc list-inside space-y-1 ml-4 mb-3">
          <li>If any platform shows Red status, investigate the Failed and API Errors counts immediately. Check authentication tokens, API rate limits, and platform status pages.</li>
          <li>If Service Uptime drops below 99%, review error logs to identify patterns. Common causes include database connection issues, external API outages, or infrastructure problems.</li>
          <li>A growing Queue Backlog requires scaling resources or investigating why jobs are processing slowly. Check job processor logs and system resource utilization.</li>
          <li>High Retry counts on a platform may indicate rate limiting or transient network issues. Consider implementing exponential backoff or rate limit handling.</li>
        </ul>
        <p>
          <strong>Data Sources:</strong> All metrics are calculated in real-time from the `interactions`, `log_entries`, and `scheduled_posts` database tables. The data reflects actual activity and errors recorded in the system, not estimates or cached values. The period selector (24h or 7d) filters all queries to only include data from the selected time window.
        </p>
      </DataInterpretation>

      <SystemHealthSection period={period} startDate={startDate} endDate={endDate} />
    </div>
  )
}

