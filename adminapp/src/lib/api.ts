// API client for admin dashboard
import type {
  DashboardData,
  SystemHealthData,
  AIBehaviorData,
  CostControlData,
  TenantOverviewData,
  MessagingActivityData,
  PostingStatusData,
  QualitativeInsightsData,
} from '@/types/dashboard'

// Admin API base - all admin endpoints should use this prefix
const API_BASE = '/admin/api'

class ApiClient {
  private async request<T>(
    endpoint: string,
    options?: RequestInit
  ): Promise<T> {
    const response = await fetch(`${API_BASE}${endpoint}`, {
      ...options,
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
        ...options?.headers,
      },
    })

    if (!response.ok) {
      if (response.status === 401) {
        window.location.href = '/admin/login'
        throw new Error('Unauthorized')
      }
      const errorText = await response.text()
      throw new Error(`API error (${response.status}): ${errorText || response.statusText}`)
    }

    // Handle empty responses
    const text = await response.text()
    if (!text) {
      return {} as T
    }

    try {
      return JSON.parse(text)
    } catch (e) {
      console.error('Failed to parse JSON:', text)
      return {} as T
    }
  }

  async getDashboard(period: '24h' | '7d' | '30d' | 'custom' = '24h', startDate?: string, endDate?: string): Promise<DashboardData> {
    const params = new URLSearchParams({ period })
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    return this.request<DashboardData>(`/dashboard/v2?${params.toString()}`)
  }

  async getSystemHealth(period: '24h' | '7d' | '30d' | 'custom' = '24h', startDate?: string, endDate?: string): Promise<SystemHealthData> {
    const params = new URLSearchParams({ period })
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    return this.request<SystemHealthData>(`/dashboard/system-health?${params.toString()}`)
  }

  async getAIBehavior(period: '24h' | '7d' | '30d' | 'custom' = '24h', startDate?: string, endDate?: string): Promise<AIBehaviorData> {
    const params = new URLSearchParams({ period })
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    return this.request<AIBehaviorData>(`/dashboard/ai-behavior?${params.toString()}`)
  }

  async getTenantOverview(limit?: number, offset?: number): Promise<TenantOverviewData> {
    const params = new URLSearchParams()
    if (limit) params.append('limit', limit.toString())
    if (offset) params.append('offset', offset.toString())
    return this.request<TenantOverviewData>(`/dashboard/tenant-overview?${params.toString()}`)
  }

  async getMessagingActivity(period: '24h' | '7d' | '30d' | 'custom' = '24h', startDate?: string, endDate?: string): Promise<MessagingActivityData> {
    const params = new URLSearchParams({ period })
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    return this.request<MessagingActivityData>(`/dashboard/messaging-activity?${params.toString()}`)
  }

  async getPostingStatus(period: '24h' | '7d' | '30d' | 'custom' = '24h', startDate?: string, endDate?: string): Promise<PostingStatusData> {
    const params = new URLSearchParams({ period })
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    return this.request<PostingStatusData>(`/dashboard/posting-status?${params.toString()}`)
  }

  async getCostControl(period: '24h' | '7d' | '30d' | 'custom' = '24h', startDate?: string, endDate?: string): Promise<CostControlData> {
    const params = new URLSearchParams({ period })
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    return this.request<CostControlData>(`/dashboard/cost-control?${params.toString()}`)
  }

  // Cost Control Settings
  async getCostControlSettings(): Promise<{
    max_ai_calls_per_min: number
  }> {
    return this.request<{ max_ai_calls_per_min: number }>('/cost-control/settings')
  }

  async updateCostControlSettings(settings: {
    max_ai_calls_per_min: number
  }): Promise<{
    max_ai_calls_per_min: number
  }> {
    return this.request<{ max_ai_calls_per_min: number }>('/cost-control/settings', {
      method: 'POST',
      body: JSON.stringify(settings),
    })
  }

  // Gupshup WhatsApp Usage & Cost Control
  async getGupshupUsage(params: {
    tenant_id: string
    from_date?: string
    to_date?: string
  }): Promise<{
    usage: Array<{
      id: string
      date: string
      gupshup_app_id: string
      authentication: number
      cumulative_bill: number
      currency: string
      discount: number
      fep: number
      ftc: number
      gs_cap: number
      gs_fees: number
      incoming_msg: number
      outgoing_msg: number
      outgoing_media_msg: number
      marketing: number
      service: number
      template_msg: number
      template_media_msg: number
      total_fees: number
      total_msg: number
      utility: number
      wa_fees: number
      international_authentication: number
      created_at: string
    }>
    from_date: string
    to_date: string
    count: number
  }> {
    const queryParams = new URLSearchParams({
      tenant_id: params.tenant_id,
    })
    if (params.from_date) queryParams.set('from_date', params.from_date)
    if (params.to_date) queryParams.set('to_date', params.to_date)
    return this.request(`/gupshup-usage?${queryParams.toString()}`)
  }

  async getGupshupDiscount(params: {
    tenant_id: string
    year?: number
    month?: number
  }): Promise<{
    discounts: Array<{
      id: string
      gupshup_app_id: string
      year: number
      month: number
      day: number
      cumulative_bill: number
      daily_bill: number
      discount: number
      gs_cap: number
      gs_fees: number
      partner_id: string
      created_at: string
    }>
    year: number
    month: number
    count: number
  }> {
    const queryParams = new URLSearchParams({
      tenant_id: params.tenant_id,
    })
    if (params.year) queryParams.set('year', params.year.toString())
    if (params.month) queryParams.set('month', params.month.toString())
    return this.request(`/gupshup-discount?${queryParams.toString()}`)
  }

  async syncGupshupUsage(params: {
    tenant_id: string
    app_id: string
    from_date?: string
    to_date?: string
  }): Promise<{
    success: boolean
    message: string
  }> {
    return this.request('/gupshup-usage/sync', {
      method: 'POST',
      body: JSON.stringify(params),
    })
  }

  async getQualitativeInsights(period: '24h' | '7d' | '30d' | 'custom' = '24h', startDate?: string, endDate?: string): Promise<QualitativeInsightsData> {
    const params = new URLSearchParams({ period })
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    return this.request<QualitativeInsightsData>(`/dashboard/qualitative-insights?${params.toString()}`)
  }

  // Actions
  async tagTenant(tenantId: string, tags: string[]) {
    return this.request(`/tenants/${tenantId}/tags`, {
      method: 'POST',
      body: JSON.stringify({ tags }),
    })
  }

  async pauseTenant(tenantId: string, paused: boolean) {
    return this.request(`/tenants/${tenantId}/pause`, {
      method: 'POST',
      body: JSON.stringify({ paused }),
    })
  }

  async createInsight(data: {
    insight_type: string
    content: string
    tenant_id?: string
    tags?: string[]
  }) {
    return this.request('/insights', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  }

  // Admin user management
  async getMe(): Promise<{ email: string; role: string }> {
    return this.request<{ email: string; role: string }>('/me')
  }

  async getAdminUsers(): Promise<AdminUser[]> {
    return this.request<AdminUser[]>('/admin-users')
  }

  async createAdminUser(data: {
    email: string
    role: string
    password: string
  }) {
    return this.request('/admin-users', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  }

  async updateAdminUser(id: string, data: {
    role?: string
    active?: boolean
    password?: string
  }) {
    return this.request(`/admin-users/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    })
  }

  // Scheduled Posts
  async getScheduledPosts(status?: string, tenantId?: string): Promise<{ total: number; items: any[] }> {
    const params = new URLSearchParams()
    if (status) params.append('status', status)
    if (tenantId) params.append('tenant_id', tenantId)
    return this.request<{ total: number; items: any[] }>(`/scheduled-posts?${params.toString()}`)
  }

  async cancelScheduledPost(id: string, reason: string) {
    return this.request(`/scheduled-posts/${id}/cancel`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    })
  }

  // Escalations
  async getEscalations(status: string = 'pending', tenantId?: string): Promise<{ total: number; items: any[] }> {
    const params = new URLSearchParams({ status })
    if (tenantId) params.append('tenant_id', tenantId)
    return this.request<{ total: number; items: any[] }>(`/escalations?${params.toString()}`)
  }

  async getEscalationMetrics(period: string = '30d', startDate?: string, endDate?: string): Promise<{
    total_tenants: number
    metrics: Array<{
      tenant_id: string
      tenant_name: string
      total_escalations: number
      pending_count: number
      resolved_count: number
      failed_count: number
      success_rate: number
      last_escalation_at?: string
    }>
  }> {
    const params = new URLSearchParams({ period })
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    return this.request(`/escalations/metrics?${params.toString()}`)
  }

  async resolveEscalation(id: string, action: 'resolve' | 'ignore') {
    return this.request(`/escalations/${id}/resolve`, {
      method: 'POST',
      body: JSON.stringify({ action }),
    })
  }

  // Failures
  async getFailures(type?: string): Promise<any> {
    const params = type ? new URLSearchParams({ type }) : new URLSearchParams()
    return this.request<any>(`/failures?${params.toString()}`)
  }

  // Analytics
  async getAnalytics(period: '24h' | '7d' | '30d' | '90d' | 'custom' = '30d', startDate?: string, endDate?: string): Promise<{
    total_posts: number
    total_engagement: number
    top_platforms: Array<{ platform: string; post_count: number; engagement: number }>
    top_tenants: Array<{ tenant_id: string; tenant_name: string; post_count: number; engagement: number }>
    recent_posts: Array<{
      id: string
      tenant_id: string
      tenant_name: string
      platform: string
      posted_at: string
      likes: number
      comments: number
      shares: number
    }>
  }> {
    const params = new URLSearchParams({ period })
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    return this.request<{
      total_posts: number
      total_engagement: number
      top_platforms: Array<{ platform: string; post_count: number; engagement: number }>
      top_tenants: Array<{ tenant_id: string; tenant_name: string; post_count: number; engagement: number }>
      recent_posts: Array<{
        id: string
        tenant_id: string
        tenant_name: string
        platform: string
        posted_at: string
        likes: number
        comments: number
        shares: number
      }>
    }>(`/analytics?${params.toString()}`)
  }

  async getTenantAnalytics(tenantId: string, period: '7d' | '30d' | '90d' = '30d') {
    return this.request(`/analytics/tenant?tenant_id=${tenantId}&period=${period}`)
  }

  // Social Accounts
  async getSocialAccounts(platform?: string, connectedOnly?: boolean, tenantId?: string, limit?: number, offset?: number): Promise<{ total: number; items: any[]; limit: number; offset: number }> {
    const params = new URLSearchParams()
    if (platform) params.append('platform', platform)
    if (connectedOnly) params.append('connected', 'true')
    if (tenantId) params.append('tenant_id', tenantId)
    if (limit) params.append('limit', limit.toString())
    if (offset) params.append('offset', offset.toString())
    return this.request<{ total: number; items: any[]; limit: number; offset: number }>(`/social-accounts?${params.toString()}`)
  }

  // Payments (Tenant-to-Brancr)
  async getPayments(status?: string, tenantId?: string, period?: string): Promise<{
    total: number
    items: any[]
    summary: {
      total_revenue: number
      this_month_revenue: number
      failed_payments: number
      pending_payments: number
      active_subscriptions: number
    }
  }> {
    const params = new URLSearchParams()
    if (status) params.append('status', status)
    if (tenantId) params.append('tenant_id', tenantId)
    if (period) params.append('period', period)
    return this.request<{
      total: number
      items: any[]
      summary: {
        total_revenue: number
        this_month_revenue: number
        failed_payments: number
        pending_payments: number
        active_subscriptions: number
      }
    }>(`/payments?${params.toString()}`)
  }

  // Health & Logs
  async getHealth(): Promise<{
    DB?: string
    Redis?: string
    Telegram?: string
    Cloudinary?: string
    OpenRouter?: string
    Notes?: Record<string, string>
    Timestamp?: string
  }> {
    return this.request<{
      DB?: string
      Redis?: string
      Telegram?: string
      Cloudinary?: string
      OpenRouter?: string
      Notes?: Record<string, string>
      Timestamp?: string
    }>('/health')
  }

  async getLogs(level?: string, tenantId?: string, limit?: number, startDate?: string, endDate?: string): Promise<{ total: number; items: any[] }> {
    const params = new URLSearchParams()
    if (level) params.append('level', level)
    if (tenantId) params.append('tenant_id', tenantId)
    if (limit) params.append('limit', limit.toString())
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    return this.request<{ total: number; items: any[] }>(`/logs?${params.toString()}`)
  }

  // Audit Logs
  async getAuditLogs(adminEmail?: string, action?: string, tenantId?: string, limit?: number, startDate?: string, endDate?: string): Promise<any[]> {
    const params = new URLSearchParams()
    if (adminEmail) params.append('admin_email', adminEmail)
    if (action) params.append('action', action)
    if (tenantId) params.append('tenant_id', tenantId)
    if (limit) params.append('limit', limit.toString())
    if (startDate) params.append('start_date', startDate)
    if (endDate) params.append('end_date', endDate)
    const queryString = params.toString()
    return this.request<any[]>(`/audit${queryString ? `?${queryString}` : ''}`)
  }

  // Tenants
  async getTenants(status?: string, search?: string, limit?: number, offset?: number): Promise<{
    total: number
    items: Array<{
      id: string
      name: string
      email: string
      plan_type: string
      status: string
      onboarding_step: string
      onboarding_complete: boolean
      platforms: string[]
      last_activity?: string
      usage_percent: number
      ai_human_mode: string
      kyb_status?: string
      admin_tags?: string[]
      pending_escalations: number
      created_at: string
    }>
    limit: number
    offset: number
  }> {
    const params = new URLSearchParams()
    if (status) params.append('status', status)
    if (search) params.append('q', search)
    if (limit) params.append('limit', limit.toString())
    if (offset) params.append('offset', offset.toString())
    return this.request(`/tenants?${params.toString()}`)
  }

  async getTenantDetail(id: string): Promise<{
    tenant: {
      id: string
      name: string
      email: string
      plan_type: string
      status: string
      blocked: boolean
      suspended: boolean
      onboarding_step: string
      onboarding_complete: boolean
      ai_human_mode: string
      industry: string
      admin_tags: string[]
      kyb_status: string
      created_at: string
    }
    quota: any
    social_accounts: any[]
    scheduled_posts: any[]
    pending_escalations: number
    stats: {
      total_interactions: number
      messages_last_7_days: number
      escalations_count: number
      failed_sends: number
      success_rate: number
      last_activity_at?: string
    }
  }> {
    return this.request(`/tenants/${id}`)
  }

  // Alerts
  async sendAlert(data: {
    tenant_ids?: string[]
    plan_type?: string
    status?: string
    type: string
    severity: string
    title: string
    message: string
    action_url?: string
    action_label?: string
  }): Promise<{ status: string; sent: number; failed: number }> {
    return this.request('/alerts', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  }

  async getAlerts(tenantId?: string): Promise<any[]> {
    // Admin alerts endpoint: GET /admin/api/alerts?tenant_id=X
    const params = tenantId ? new URLSearchParams({ tenant_id: tenantId }) : new URLSearchParams()
    const endpoint = `/alerts${params.toString() ? `?${params.toString()}` : ''}`
    return this.request<any[]>(endpoint)
  }

  // Alert Templates (Editable automated messages)
  async getAlertTemplates(type?: string): Promise<AlertTemplate[]> {
    const params = type ? new URLSearchParams({ type }) : new URLSearchParams()
    return this.request<AlertTemplate[]>(`/alert-templates${params.toString() ? `?${params.toString()}` : ''}`)
  }

  async getAlertTemplate(id: string): Promise<AlertTemplate> {
    return this.request<AlertTemplate>(`/alert-templates/get?id=${id}`)
  }

  async updateAlertTemplate(id: string, data: {
    title: string
    message: string
    action_url?: string
    action_label?: string
    is_active: boolean
  }): Promise<{ status: string; message: string }> {
    return this.request<{ status: string; message: string }>(`/alert-templates?id=${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    })
  }

  async resetAlertTemplate(key: string): Promise<{ status: string; message: string }> {
    return this.request<{ status: string; message: string }>(`/alert-templates/reset?key=${key}`, {
      method: 'POST',
    })
  }

  // Subscriptions
  async getTenantsByPlanType(planType?: string, status?: string): Promise<{
    total: number
    tenants: Array<{
      id: string
      name: string
      email: string
      plan_type: string
      status: string
      subscription_expires_at?: string
      trial_ends_at?: string
    }>
  }> {
    const params = new URLSearchParams()
    if (planType) params.append('plan_type', planType)
    if (status) params.append('status', status)
    return this.request(`/subscriptions/tenants?${params.toString()}`)
  }

  // Newsletters
  async createNewsletter(data: {
    title: string
    subject: string
    content: string
    categories?: string[]
    scheduled_at?: string
  }): Promise<any> {
    return this.request('/newsletters', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  }

  async sendNewsletter(id: string): Promise<{ status: string; message: string }> {
    return this.request(`/newsletters/send?id=${id}`, {
      method: 'POST',
    })
  }

  async getNewsletters(): Promise<any[]> {
    return this.request<any[]>('/newsletters')
  }

  async getNewsletterSubscriptions(): Promise<any[]> {
    return this.request<any[]>('/newsletters/subscriptions')
  }

  // Plan Templates
  async getPlanTemplates(includeDeprecated?: boolean): Promise<PlanTemplate[]> {
    const params = new URLSearchParams()
    if (includeDeprecated) params.append('include_deprecated', 'true')
    return this.request<PlanTemplate[]>(`/plans/templates?${params.toString()}`)
  }

  async createPlanTemplate(data: {
    name: string
    default_ai_reply_quota: number
    default_whatsapp_message_quota: number
    default_post_quota: number
    default_storage_limit_mb: number
    default_api_calls_limit: number
    default_platforms_limit: number
    ai_negotiation_enabled: boolean
    escalation_behavior: string
    overages_allowed: boolean
    default_monthly_price: number
    description?: string
  }): Promise<PlanTemplate> {
    return this.request<PlanTemplate>('/plans/templates', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  }

  async updatePlanTemplate(id: string, data: Partial<PlanTemplate>): Promise<{ status: string }> {
    return this.request<{ status: string }>(`/plans/templates/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    })
  }

  async deprecatePlanTemplate(id: string): Promise<{ status: string }> {
    return this.request<{ status: string }>(`/plans/templates/${id}/deprecate`, {
      method: 'POST',
    })
  }

  // Tenant Plan Management
  async assignPlanToTenant(tenantId: string, data: {
    plan_template_id: string
    override_price?: number
  }): Promise<TenantPlan> {
    return this.request<TenantPlan>(`/tenants/${tenantId}/plans/assign`, {
      method: 'POST',
      body: JSON.stringify(data),
    })
  }

  async getTenantBilling(tenantId: string): Promise<TenantBillingData> {
    return this.request<TenantBillingData>(`/tenants/${tenantId}/billing`)
  }

  async updateTenantPlanQuotas(tenantId: string, quotas: {
    ai_reply_quota?: number
    whatsapp_message_quota?: number
    post_quota?: number
    storage_limit_mb?: number
    api_calls_limit?: number
    platforms_limit?: number
  }): Promise<{ status: string }> {
    return this.request<{ status: string }>(`/tenants/${tenantId}/plans/quotas`, {
      method: 'PUT',
      body: JSON.stringify(quotas),
    })
  }

  async updateTenantPlanPrice(tenantId: string, price: number): Promise<{ status: string }> {
    return this.request<{ status: string }>(`/tenants/${tenantId}/plans/price`, {
      method: 'PUT',
      body: JSON.stringify({ price }),
    })
  }

  async pauseTenantPlan(tenantId: string): Promise<{ status: string }> {
    return this.request<{ status: string }>(`/tenants/${tenantId}/plans/pause`, {
      method: 'POST',
    })
  }

  async resumeTenantPlan(tenantId: string): Promise<{ status: string }> {
    return this.request<{ status: string }>(`/tenants/${tenantId}/plans/resume`, {
      method: 'POST',
    })
  }

  // Tenant Management (SuperAdmin only)
  async deleteTenant(tenantId: string): Promise<{ status: string; message: string }> {
    return this.request<{ status: string; message: string }>(`/tenants/${tenantId}`, {
      method: 'DELETE',
    })
  }

  async blockTenant(tenantId: string): Promise<{ status: string; message: string }> {
    return this.request<{ status: string; message: string }>(`/tenants/${tenantId}/block`, {
      method: 'POST',
    })
  }

  async unblockTenant(tenantId: string): Promise<{ status: string; message: string }> {
    return this.request<{ status: string; message: string }>(`/tenants/${tenantId}/unblock`, {
      method: 'POST',
    })
  }

  async suspendTenant(tenantId: string): Promise<{ status: string; message: string }> {
    return this.request<{ status: string; message: string }>(`/tenants/${tenantId}/suspend`, {
      method: 'POST',
    })
  }

  async unsuspendTenant(tenantId: string): Promise<{ status: string; message: string }> {
    return this.request<{ status: string; message: string }>(`/tenants/${tenantId}/unsuspend`, {
      method: 'POST',
    })
  }
  // Experience Checkpoints
  async getCheckpointStats(): Promise<{
    total: number
    responded: number
    active_pending: number
    by_type: Record<string, number>
    trust_scores: Record<number, number>
  }> {
    return this.request('/checkpoints/stats')
  }

  async getCheckpointResponses(params: { event_type?: string } = {}): Promise<{
    responses: Array<{
      id: string
      tenant_id: string
      tenant_name: string
      event_type: string
      selected_reason: string
      trust_score?: number
      free_text?: string
      created_at: string
    }>
  }> {
    const q = new URLSearchParams()
    if (params.event_type) q.append('event_type', params.event_type)
    return this.request(`/checkpoints/responses?${q.toString()}`)
  }

  async triggerCheckpoint(targetType: 'individual' | 'group', targetValue: string, eventType: string): Promise<{ success: boolean; message: string; target_count?: number }> {
    return this.request('/checkpoints/trigger', {
      method: 'POST',
      body: JSON.stringify({
        target_type: targetType,
        target_value: targetValue,
        event_type: eventType
      }),
    })
  }
}

export interface PlanTemplate {
  id: string
  name: string
  default_ai_reply_quota: number
  default_whatsapp_message_quota: number
  default_post_quota: number
  default_storage_limit_mb: number
  default_api_calls_limit: number
  default_platforms_limit: number
  ai_negotiation_enabled: boolean
  escalation_behavior: string
  overages_allowed: boolean
  default_monthly_price: number
  status: string
  description: string
  created_at: string
  updated_at: string
}

export interface TenantPlan {
  id: string
  tenant_id: string
  plan_template_id: string
  plan_name: string
  ai_reply_quota: number
  whatsapp_message_quota: number
  post_quota: number
  storage_limit_mb: number
  api_calls_limit: number
  platforms_limit: number
  ai_negotiation_enabled: boolean
  escalation_behavior: string
  overages_allowed: boolean
  monthly_price: number
  billing_status: string
  payment_status: string
  billing_cycle_start: string
  billing_cycle_end?: string
  next_billing_date?: string
  trial_ends_at?: string
  quotas_overridden: boolean
  price_overridden: boolean
}

export interface TenantBillingData {
  plan: {
    id: string
    plan_name: string
    plan_template_id: string
    ai_reply_quota: number
    whatsapp_message_quota: number
    post_quota: number
    storage_limit_mb: number
    api_calls_limit: number
    platforms_limit: number
    ai_negotiation_enabled: boolean
    escalation_behavior: string
    overages_allowed: boolean
    monthly_price: number
    billing_status: string
    payment_status: string
    billing_cycle_start: string
    billing_cycle_end?: string
    next_billing_date?: string
    trial_ends_at?: string
    quotas_overridden: boolean
    price_overridden: boolean
  } | null
  usage: {
    daily: {
      ai_reply: { used: number; limit: number; percent: number; status: string }
      whatsapp_message: { used: number; limit: number; percent: number; status: string }
      post: { used: number; limit: number; percent: number; status: string }
    }
    monthly: {
      ai_reply_count: number
      ai_tokens_used: number
      whatsapp_message_count: number
      post_count: number
      total_cost_usd: number
    }
  } | null
}

export interface AdminUser {
  id: string
  email: string
  role: string
  active: boolean
  last_login?: string
  created_at: string
}

export interface AlertTemplate {
  id: string
  key: string
  name: string
  type: string
  severity: string
  title: string
  message: string
  action_url: string
  action_label: string
  is_active: boolean
  is_editable: boolean
  description: string
  updated_at: string
}

export const api = new ApiClient()

