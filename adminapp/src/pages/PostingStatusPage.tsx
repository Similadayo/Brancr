import { useState } from 'react'
import PostingStatusSection from '@/components/dashboard/PostingStatusSection'
import { DataInterpretation } from '@/components/ui/data-interpretation'
import { DateRangeFilter, DatePeriod } from '@/components/ui/date-range-filter'

export default function PostingStatusPage() {
  const [period, setPeriod] = useState<DatePeriod>('24h')
  const [startDate, setStartDate] = useState<string>('')
  const [endDate, setEndDate] = useState<string>('')

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold text-gray-900">Posting Status</h1>
          <p className="text-sm sm:text-base text-gray-600 mt-1">Post publishing success and failure tracking</p>
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
        title="Understanding Posting Status Metrics"
        type="info"
      >
        <p className="mb-3">
          Posting Status tracks the success and failure of social media posts scheduled through Brancr. This helps identify publishing issues, platform-specific problems, and ensures tenants' content is successfully delivered to their audiences.
        </p>
        <p className="mb-2 font-semibold">Key Metrics:</p>
        <ul className="list-disc list-inside space-y-2 ml-2 mb-3">
          <li><strong>Posts Scheduled:</strong> Total number of posts that were scheduled to be published during the period. This includes posts scheduled for Instagram, Facebook, TikTok, and other supported platforms.</li>
          <li><strong>Posts Published:</strong> Number of posts that were successfully published to their target platforms. A high success rate indicates the posting system is working well.</li>
          <li><strong>Posts Failed:</strong> Number of posts that failed to publish. Failures can occur due to authentication issues, API errors, content policy violations, network problems, or platform-specific restrictions. Each failed post should be investigated to prevent similar issues.</li>
          <li><strong>Success Rate:</strong> Percentage of scheduled posts that were successfully published. Calculated as: `(Published / Scheduled) * 100`. Target success rates should be &gt; 95%. Lower rates indicate systemic posting issues that need attention.</li>
          <li><strong>Posts by Platform:</strong> Breakdown of posting activity by social media platform. This shows which platforms tenants are using most for content distribution and helps identify platform-specific issues.</li>
          <li><strong>Pending Posts:</strong> Number of posts that are scheduled but not yet published (their scheduled time hasn't arrived). These are posts waiting in the queue for their scheduled publish time.</li>
          <li><strong>Retries:</strong> Number of posts that were retried after an initial failure. Automatic retries help handle transient failures (network issues, temporary API outages), but persistent retries indicate deeper problems.</li>
        </ul>
        <p className="mb-2 font-semibold">Common Failure Reasons:</p>
        <ul className="list-disc list-inside space-y-1 ml-2 mb-3">
          <li><strong>Authentication Errors:</strong> Expired or invalid OAuth tokens. These require token refresh or re-authentication by the tenant.</li>
          <li><strong>API Errors:</strong> Platform API rate limits, temporary outages, or API version changes. These are usually transient and may resolve automatically.</li>
          <li><strong>Content Policy Violations:</strong> Posts that violate platform content policies (e.g., Instagram's community guidelines). These posts will be rejected by the platform and cannot be republished without modification.</li>
          <li><strong>Network/Timeout Errors:</strong> Network connectivity issues or request timeouts. These are usually transient and will be retried automatically.</li>
          <li><strong>Account Issues:</strong> Suspended or restricted accounts, missing permissions, or account access revoked. These require tenant action to resolve.</li>
        </ul>
        <p className="mb-2">
          <strong>Action Items:</strong>
        </p>
        <ul className="list-disc list-inside space-y-1 ml-4 mb-3">
          <li>If success rate drops below 95%, investigate the most common failure reasons and address root causes (token refresh, API changes, content policy issues).</li>
          <li>High failure rates on a specific platform may indicate API changes, platform-specific issues, or authentication problems that need platform-wide attention.</li>
          <li>Posts that fail due to content policy violations should be flagged to tenants with specific guidance on what content was rejected and why.</li>
          <li>Authentication errors require proactive token refresh management - set up alerts for tokens expiring soon to prevent posting failures.</li>
          <li>Review retry counts - high retry counts with eventual success indicate transient issues, while persistent failures after retries indicate permanent problems that need manual intervention.</li>
        </ul>
        <p>
          <strong>Data Source:</strong> All metrics are calculated from the `scheduled_posts` table, filtering by `created_at` or `scheduled_at` within the selected period. Post status is tracked in the `status` field (pending, published, failed, cancelled). Failed posts have error details stored in the `last_error` field, which can be used to diagnose and fix issues.
        </p>
      </DataInterpretation>

      <PostingStatusSection period={period} startDate={startDate} endDate={endDate} />
    </div>
  )
}

