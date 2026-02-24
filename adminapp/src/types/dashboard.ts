// Dashboard API response types

export interface PlatformHealth {
  name: string
  messages_received: number
  messages_sent: number
  failed_sends: number
  retry_count: number
  api_errors: number
  status: 'green' | 'amber' | 'red'
}

export interface SystemMetrics {
  background_job_failures: number
  queue_backlog_size: number
  last_incident_timestamp?: string
  service_uptime: string
}

export interface SystemHealthData {
  platforms: PlatformHealth[]
  system: SystemMetrics
  last_incident?: string
}

export interface AIBehaviorData {
  ai_replies_sent: number
  total_ai_calls?: number
  escalations_triggered: number
  escalation_rate: number
  manual_overrides: number
  ai_disabled_events: number
  avg_confidence_level: number
  interpretations: string[]
}

export interface TenantCost {
  tenant_id: string
  tenant_name: string
  cost: number
  tokens_used: number
}

export interface CostAlert {
  type: 'spike' | 'threshold' | 'outlier'
  message: string
  tenant_id?: string
}

export interface CostControlData {
  openai_tokens_used: number
  estimated_ai_cost: number
  whatsapp_messages_sent: number
  whatsapp_cost: number
  total_cost: number
  cost_per_tenant: TenantCost[]
  top_cost_tenants: TenantCost[]
  alerts: CostAlert[]
}

export interface TenantOverviewItem {
  id: string
  name: string
  industry: string
  platforms_connected: string[]
  onboarding_status: string
  time_to_first_value: string
  ai_enabled: boolean
  last_activity?: string
  industry_change_count: number
  admin_tags: string[]
}

export interface TenantOverviewSummary {
  total_tenants: number
  active_tenants: number
  onboarding_complete: number
  ai_enabled_count: number
  needs_followup: number
}

export interface TenantOverviewData {
  tenants: TenantOverviewItem[]
  summary: TenantOverviewSummary
}

export interface PlatformCount {
  platform: string
  count: number
}

export interface HourVolume {
  hour: number
  count: number
}

export interface MessagingActivityData {
  total_conversations: number
  total_messages?: number
  messages_by_ai: number
  messages_escalated: number
  messages_per_platform: PlatformCount[]
  peak_hours: HourVolume[]
}

export interface FailureReason {
  reason: string
  count: number
}

export interface PostingStatusData {
  posts_scheduled: number
  posts_published: number
  failed_posts: number
  failure_reasons: FailureReason[]
}

export interface InsightItem {
  id: string
  content: string
  tenant_id?: string
  conversation_id?: string
  admin_email: string
  tags: string[]
  created_at: string
}

export interface QualitativeInsightsData {
  top_complaints: InsightItem[]
  top_aha_moments: InsightItem[]
  repeated_objections: InsightItem[]
  risky_escalations: InsightItem[]
  onboarding_notes: InsightItem[]
}

export interface DashboardData {
  system_health?: SystemHealthData
  ai_behavior?: AIBehaviorData
  tenant_overview?: TenantOverviewData
  messaging_activity?: MessagingActivityData
  posting_status?: PostingStatusData
  cost_control?: CostControlData
  qualitative_insights?: QualitativeInsightsData
}

