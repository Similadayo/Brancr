import { useState } from 'react'
import MessagingActivitySection from '@/components/dashboard/MessagingActivitySection'
import { DataInterpretation } from '@/components/ui/data-interpretation'
import { DateRangeFilter, DatePeriod } from '@/components/ui/date-range-filter'

export default function MessagingPage() {
  const [period, setPeriod] = useState<DatePeriod>('24h')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold text-gray-900">Messaging Activity</h1>
          <p className="text-sm sm:text-base text-gray-600 mt-1">Core value metrics — conversations and messages</p>
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

      <DataInterpretation
        title="Understanding Messaging Activity Metrics"
        type="info"
      >
        <p className="mb-3">
          Messaging Activity shows the core value metrics for Brancr: how many conversations are happening, how many messages are being sent and received, and how much of the messaging is automated by AI vs. handled manually by tenant staff. This is the primary indicator of platform usage and value delivery.
        </p>
        <p className="mb-2 font-semibold">Key Metrics:</p>
        <ul className="list-disc list-inside space-y-2 ml-2 mb-3">
          <li><strong>Total Conversations:</strong> Number of distinct conversation threads between customers and tenants during the period. Each conversation represents a customer interaction that may span multiple messages. A high conversation count indicates active customer engagement across all tenants.</li>
          <li><strong>Total Messages:</strong> Total count of all messages (both inbound from customers and outbound responses) during the period. This is the sum of all messages across all platforms (Instagram, Facebook, WhatsApp, TikTok). Higher message volumes indicate more customer activity and engagement.</li>
          <li><strong>Messages by Platform:</strong> Breakdown of message volume by social media platform. This helps identify which platforms are most active and where tenants are getting the most customer engagement. Use this to understand platform preferences and prioritize platform-specific features.</li>
          <li><strong>AI Percent:</strong> Percentage of messages that were automatically generated and sent by AI without human intervention. Calculated as: `(AI replies / Total outbound messages) * 100`. Higher AI percentages indicate the AI is handling more customer interactions autonomously, reducing manual workload for tenants.</li>
          <li><strong>Human Percent:</strong> Percentage of messages that were manually sent by tenant staff. Calculated as: `100 - AI Percent`. Lower human percentages indicate the AI is successfully handling most interactions, while higher percentages may indicate tenants prefer manual control or AI is not being used.</li>
          <li><strong>Failed Deliveries:</strong> Number of outbound messages that failed to send. This could be due to API errors, authentication issues, network problems, or platform-specific restrictions. High failure rates indicate technical issues that need investigation.</li>
          <li><strong>Latency (Avg Response Time):</strong> Average time between when a customer sends a message and when they receive a response. Lower latency indicates faster response times, which improves customer satisfaction. AI responses are typically faster than manual responses.</li>
        </ul>
        <p className="mb-2 font-semibold">Hourly Volume Chart:</p>
        <p className="mb-3 ml-2">
          The hourly volume chart shows message distribution throughout the day, helping identify peak activity hours, timezone patterns, and when customers are most engaged. This data can inform optimal posting schedules and staffing decisions for tenant businesses.
        </p>
        <p className="mb-2">
          <strong>Action Items:</strong>
        </p>
        <ul className="list-disc list-inside space-y-1 ml-4 mb-3">
          <li>If AI Percent is low, investigate why tenants are not using AI features - this may indicate usability issues, trust concerns, or feature gaps.</li>
          <li>High failed delivery rates require immediate investigation of platform integrations, authentication tokens, and API status.</li>
          <li>High latency (slow response times) can hurt customer satisfaction - investigate if this is due to AI processing delays, manual response times, or system bottlenecks.</li>
          <li>Use hourly volume patterns to identify peak engagement times and recommend optimal posting schedules to tenants.</li>
          <li>Monitor platform distribution to understand which platforms are most valuable for tenants and prioritize platform-specific improvements.</li>
        </ul>
        <p>
          <strong>Data Source:</strong> All metrics are calculated from the `interactions` and `conversations` tables, counting records within the selected time period (24h or 7d). Messages are identified by their `direction` field (inbound/outbound) and `response_type` field (auto_reply for AI, manual for human). Failed deliveries are counted from interactions with `response_status IN ("failed", "error")`.
        </p>
      </DataInterpretation>

      <MessagingActivitySection period={period} startDate={startDate} endDate={endDate} />
    </div>
  )
}

