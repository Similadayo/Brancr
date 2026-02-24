import { useState } from 'react'
import QualitativeInsightsSection from '@/components/dashboard/QualitativeInsightsSection'
import { Button } from '@/components/ui/button'
import { DataInterpretation } from '@/components/ui/data-interpretation'

export default function InsightsPage() {
  const [period, setPeriod] = useState<'24h' | '7d'>('24h')

  return (
      <div className="space-y-6">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-gray-900">Qualitative Insights</h1>
            <p className="text-sm sm:text-base text-gray-600 mt-1">Manual notes, complaints, and risk indicators</p>
          </div>
          <div className="flex gap-2">
            <Button
              variant={period === '24h' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setPeriod('24h')}
              className="text-xs sm:text-sm"
            >
              Last 24h
            </Button>
            <Button
              variant={period === '7d' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setPeriod('7d')}
              className="text-xs sm:text-sm"
            >
              Last 7 days
            </Button>
          </div>
        </div>

        <DataInterpretation 
          title="Understanding Qualitative Insights"
          type="info"
        >
          <p className="mb-3">
            Qualitative Insights are manual annotations, observations, and risk flags added by admin staff to track important events, complaints, customer issues, and business risks that cannot be captured by automated metrics alone. These insights provide context and human intelligence to complement quantitative dashboard data.
          </p>
          <p className="mb-2 font-semibold">Insight Types:</p>
          <ul className="list-disc list-inside space-y-2 ml-2 mb-3">
            <li><strong>Notes:</strong> General observations, tenant interactions, or important events recorded by staff. Use these to document conversations, follow-ups, or contextual information that helps understand tenant behavior or support needs.</li>
            <li><strong>Complaints:</strong> Customer complaints or escalations from tenants. These are critical for tracking satisfaction issues, identifying systemic problems, and ensuring timely resolution. Each complaint should be followed up and resolved.</li>
            <li><strong>Risk Flags:</strong> Potential business risks, compliance issues, payment problems, or behavioral patterns that may indicate fraud, abuse, or account issues. Risk flags require immediate review and action by appropriate admin roles.</li>
            <li><strong>Feedback:</strong> Tenant feedback, feature requests, or suggestions for platform improvements. These help prioritize product development and understand tenant needs.</li>
          </ul>
          <p className="mb-2 font-semibold">How to Use:</p>
          <ul className="list-disc list-inside space-y-2 ml-2 mb-3">
            <li>Staff members with appropriate roles (SupportAdmin, PlatformAdmin, ComplianceAdmin, etc.) can create insights by clicking "Add Insight" and selecting the type, tenant (if applicable), and entering the content.</li>
            <li>Insights are associated with specific tenants when relevant, or can be platform-wide/system-level observations.</li>
            <li>Tags can be added to categorize insights (e.g., "billing", "technical", "complaint", "urgent") for easy filtering and tracking.</li>
            <li>All insights are timestamped and associated with the admin user who created them for accountability and traceability.</li>
            <li>Use the period selector (24h or 7d) to filter insights to the relevant time window.</li>
          </ul>
          <p className="mb-2">
            <strong>Action Items:</strong>
          </p>
          <ul className="list-disc list-inside space-y-1 ml-4 mb-3">
            <li>Review new complaints daily and ensure each is addressed with appropriate follow-up actions.</li>
            <li>Investigate risk flags immediately - they may indicate security, compliance, or business integrity issues.</li>
            <li>Use notes to build institutional knowledge about tenant behavior, common issues, and successful resolution strategies.</li>
            <li>Regularly review feedback insights to identify product improvement opportunities and common pain points.</li>
          </ul>
          <p>
            <strong>Data Source:</strong> Insights are stored in the `admin_qualitative_insights` table and are created manually by admin staff through the dashboard interface. They are not generated automatically by the system, ensuring they represent human judgment and contextual understanding.
          </p>
        </DataInterpretation>

        <QualitativeInsightsSection period={period} />
      </div>
  )
}

