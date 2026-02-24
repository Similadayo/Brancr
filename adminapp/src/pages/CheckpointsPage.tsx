import { useState, useEffect } from 'react'
import { api } from '@/lib/api'
import { format } from 'date-fns'
import {
    Filter,
    MessageCircle,
    Send,
    AlertTriangle,
    CheckCircle2,
    Clock,
    Copy,
    Users,
    User,
    Search,
    X
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Label } from '@/components/ui/label'

// Event Type Badge Helper
const getEventBadge = (type: string) => {
    switch (type) {
        case 'first_reply':
            return <Badge variant="secondary" className="bg-blue-100 text-blue-800 hover:bg-blue-200">First Reply</Badge>
        case 'escalation':
            return <Badge variant="destructive">Escalation</Badge>
        case 'ai_toggle':
            return <Badge variant="outline" className="border-orange-500 text-orange-600">AI Toggle</Badge>
        case 'onboarding_48h':
            return <Badge variant="outline" className="border-purple-500 text-purple-600">Onboarding</Badge>
        default:
            return <Badge variant="secondary">{type || 'Unknown'}</Badge>
    }
}

// Trust Score Helper
const renderTrustScore = (score?: number) => {
    // Check exclusively for null/undefined so 0 is rendered
    if (score === undefined || score === null) return <span className="text-gray-300">-</span>
    return (
        <div className="flex gap-0.5">
            {[1, 2, 3, 4, 5].map((i) => (
                <span key={i} className={`text-sm ${i <= score ? 'text-yellow-400' : 'text-gray-200'}`}>★</span>
            ))}
        </div>
    )
}

export default function CheckpointsPage() {
    const [stats, setStats] = useState<any>(null)
    const [responses, setResponses] = useState<any[]>([])
    const [loading, setLoading] = useState(true)
    const [eventTypeFilter, setEventTypeFilter] = useState<string>('all')
    const [triggerOpen, setTriggerOpen] = useState(false)

    // Trigger Form State
    const [triggerType, setTriggerType] = useState<'individual' | 'group'>('individual')
    const [triggerValue, setTriggerValue] = useState('') // Tenant ID or Group Name
    const [triggerEventType, setTriggerEventType] = useState('first_reply')
    const [submitting, setSubmitting] = useState(false)

    // Tenant Search State
    const [tenantSearch, setTenantSearch] = useState('')
    const [tenantResults, setTenantResults] = useState<any[]>([])
    const [selectedTenantName, setSelectedTenantName] = useState('')
    const [isSearching, setIsSearching] = useState(false)

    const [resultModal, setResultModal] = useState<{
        open: boolean
        title: string
        message: string
        type: 'success' | 'error' | 'info'
    }>({ open: false, title: '', message: '', type: 'info' })

    useEffect(() => {
        loadData()
    }, [eventTypeFilter])

    // Debounced Tenant Search
    useEffect(() => {
        if (triggerType !== 'individual' || !tenantSearch) {
            setTenantResults([])
            return
        }

        // Delay search by 300ms
        const timer = setTimeout(async () => {
            setIsSearching(true)
            try {
                const res = await api.getTenants(undefined, tenantSearch, 5) // Limit to top 5
                setTenantResults(res.items || [])
            } catch (e) {
                console.error("Failed to search tenants", e)
            } finally {
                setIsSearching(false)
            }
        }, 300)

        return () => clearTimeout(timer)
    }, [tenantSearch, triggerType])

    async function loadData() {
        setLoading(true)
        try {
            const [statsData, responsesData] = await Promise.all([
                api.getCheckpointStats().catch(() => null),
                api.getCheckpointResponses({
                    event_type: eventTypeFilter === 'all' ? undefined : eventTypeFilter
                }).catch(() => ({ responses: [] }))
            ])
            setStats(statsData)
            setResponses(Array.isArray(responsesData?.responses) ? responsesData.responses : [])
        } catch (err) {
            console.error(err)
            // Silent error on load, or maybe toast? styling consistency means maybe no popup for load error unless critical
        } finally {
            setLoading(false)
        }
    }

    const selectTenant = (tenant: any) => {
        setTriggerValue(tenant.id)
        setSelectedTenantName(tenant.name)
        setTenantSearch('') // Clear search to hide list
        setTenantResults([])
    }

    const clearSelectedTenant = () => {
        setTriggerValue('')
        setSelectedTenantName('')
        setTenantSearch('')
    }

    const handleTriggerSubmit = async (e: React.FormEvent) => {
        e.preventDefault()
        setSubmitting(true)
        try {
            // Determine actual value based on type
            const finalValue = triggerType === 'individual' ? triggerValue : triggerValue

            if (!finalValue) {
                setResultModal({
                    open: true,
                    title: 'Missing Selection',
                    message: 'Please select a Target Tenant or Group.',
                    type: 'error'
                })
                setSubmitting(false)
                return
            }

            const res = await api.triggerCheckpoint(triggerType, finalValue, triggerEventType)

            let msg = `Successfully triggered checkpoint.`
            if (res.target_count !== undefined) {
                msg = `Successfully queued ${res.target_count} checkpoints.`
            } else if (triggerType === 'individual') {
                msg = `Sent ${triggerEventType} checkpoint to tenant: ${selectedTenantName || finalValue}.`
            }

            setResultModal({
                open: true,
                title: 'Success',
                message: msg,
                type: 'success'
            })
            setTriggerOpen(false)
            clearSelectedTenant()
            loadData()
        } catch (err: any) {
            setResultModal({
                open: true,
                title: 'Operation Failed',
                message: err.message || "Something went wrong.",
                type: 'error'
            })
        } finally {
            setSubmitting(false)
        }
    }

    const safeFormatDate = (dateStr: string) => {
        if (!dateStr) return '-'
        try {
            return format(new Date(dateStr), 'MMM d, yyyy HH:mm')
        } catch (e) {
            return 'Invalid Date'
        }
    }

    return (
        <div className="space-y-6 max-w-7xl mx-auto p-4 sm:p-6 pb-20">
            <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
                <div>
                    <h1 className="text-3xl font-bold tracking-tight text-gray-900">Experience Checkpoints</h1>
                    <p className="text-gray-500 mt-1">Monitor and manage automated customer feedback loops.</p>
                </div>

                <Button onClick={() => setTriggerOpen(!triggerOpen)} className="gap-2">
                    <Send className="w-4 h-4" />
                    {triggerOpen ? 'Close Trigger' : 'Trigger Checkpoint'}
                </Button>
            </div>

            {/* Custom Result Dialog (Backdrop + Modal) */}
            {resultModal.open && (
                <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm animate-in fade-in duration-200">
                    <div className="bg-white p-6 rounded-lg shadow-xl max-w-md w-full border border-gray-200 animate-in zoom-in-95 duration-200">
                        <div className="flex items-center gap-2 mb-2">
                            {resultModal.type === 'error' && <AlertTriangle className="h-5 w-5 text-red-500" />}
                            {resultModal.type === 'success' && <CheckCircle2 className="h-5 w-5 text-green-500" />}
                            {resultModal.type === 'info' && <MessageCircle className="h-5 w-5 text-blue-500" />}
                            <h3 className="text-lg font-semibold">{resultModal.title}</h3>
                        </div>
                        <p className="text-sm text-gray-500 mb-6 leading-relaxed">
                            {resultModal.message}
                        </p>
                        <div className="flex justify-end">
                            <Button onClick={() => setResultModal({ ...resultModal, open: false })}>
                                Close
                            </Button>
                        </div>
                    </div>
                </div>
            )}

            {/* Manual Trigger Form */}
            {triggerOpen && (
                <div className="bg-gray-50 p-6 rounded-lg border border-gray-200 shadow-sm animate-in slide-in-from-top-2">
                    <div className="flex items-center justify-between mb-4">
                        <h3 className="font-semibold text-lg">Trigger Manual Checkpoint</h3>
                    </div>

                    <form onSubmit={handleTriggerSubmit} className="space-y-4">
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                            {/* Target Type Selector */}
                            <div className="space-y-2">
                                <Label>Target Audience</Label>
                                <div className="flex gap-2">
                                    <Button
                                        type="button"
                                        variant={triggerType === 'individual' ? 'default' : 'outline'}
                                        onClick={() => { setTriggerType('individual'); clearSelectedTenant() }}
                                        className="flex-1"
                                    >
                                        <User className="w-4 h-4 mr-2" />
                                        Individual
                                    </Button>
                                    <Button
                                        type="button"
                                        variant={triggerType === 'group' ? 'default' : 'outline'}
                                        onClick={() => { setTriggerType('group'); setTriggerValue('new') }}
                                        className="flex-1"
                                    >
                                        <Users className="w-4 h-4 mr-2" />
                                        Group
                                    </Button>
                                </div>
                            </div>

                            {/* Dynamic Input based on Type */}
                            <div className="space-y-2 relative">
                                <Label htmlFor="targetValue">
                                    {triggerType === 'individual' ? 'Select Tenant' : 'Select Group'}
                                </Label>

                                {triggerType === 'individual' ? (
                                    <>
                                        {!triggerValue ? (
                                            <div className="relative">
                                                <Search className="absolute left-3 top-3 h-4 w-4 text-gray-400" />
                                                <Input
                                                    id="tenantSearch"
                                                    placeholder="Search by name..."
                                                    className="pl-9"
                                                    value={tenantSearch}
                                                    onChange={(e) => setTenantSearch(e.target.value)}
                                                    autoComplete="off"
                                                />
                                                {/* Autosuggestion List */}
                                                {(tenantResults.length > 0 || isSearching) && (
                                                    <div className="absolute z-10 w-full mt-1 bg-white border border-gray-200 rounded-md shadow-lg max-h-60 overflow-auto">
                                                        {isSearching && <div className="p-2 text-sm text-gray-500">Searching...</div>}
                                                        {tenantResults.map(t => (
                                                            <div
                                                                key={t.id}
                                                                className="px-4 py-2 hover:bg-gray-100 cursor-pointer text-sm"
                                                                onClick={() => selectTenant(t)}
                                                            >
                                                                <div className="font-medium text-gray-900">{t.name}</div>
                                                                <div className="text-xs text-gray-500">{t.email}</div>
                                                            </div>
                                                        ))}
                                                    </div>
                                                )}
                                            </div>
                                        ) : (
                                            <div className="flex items-center gap-2 p-2 bg-white border rounded-md">
                                                <div className="flex-1">
                                                    <div className="text-sm font-medium">{selectedTenantName || 'Selected Tenant'}</div>
                                                    <div className="text-xs text-gray-500 font-mono">{triggerValue}</div>
                                                </div>
                                                <Button type="button" variant="ghost" size="sm" onClick={clearSelectedTenant}>
                                                    <X className="h-4 w-4" />
                                                </Button>
                                            </div>
                                        )}
                                    </>
                                ) : (
                                    <select
                                        id="targetValue"
                                        className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                                        value={triggerValue}
                                        onChange={(e) => setTriggerValue(e.target.value)}
                                    >
                                        <option value="new">New Tenants (Joined &lt; 7 days)</option>
                                        <option value="active">Active Tenants</option>
                                        <option value="all">All Tenants</option>
                                    </select>
                                )}
                            </div>

                            {/* Checkpoint Type */}
                            <div className="space-y-2">
                                <Label htmlFor="eventType">Checkpoint Type</Label>
                                <select
                                    id="eventType"
                                    className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                                    value={triggerEventType}
                                    onChange={(e) => setTriggerEventType(e.target.value)}
                                >
                                    <option value="first_reply">First Reply</option>
                                    <option value="escalation">Escalation</option>
                                    <option value="ai_toggle">AI Toggle</option>
                                    <option value="onboarding_48h">Onboarding (48h)</option>
                                </select>
                            </div>

                            <div className="flex items-end">
                                <Button type="submit" disabled={submitting} className="w-full">
                                    {submitting ? 'Sending...' : 'Send Checkpoint'}
                                </Button>
                            </div>
                        </div>
                        {triggerType === 'group' && (
                            <p className="text-xs text-muted-foreground mt-2">
                                ⚠️ This will trigger checkpoints for <strong>multiple users</strong>.
                                Existing rate limits (24h cooldown) will apply to prevent spamming the same tenant twice.
                            </p>
                        )}
                    </form>
                </div>
            )}

            {/* Stats Overview */}
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                <Card>
                    <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                        <CardTitle className="text-sm font-medium">Total Triggers</CardTitle>
                        <MessageCircle className="h-4 w-4 text-muted-foreground" />
                    </CardHeader>
                    <CardContent>
                        <div className="text-2xl font-bold">{stats?.total || 0}</div>
                        <p className="text-xs text-muted-foreground">Lifetime checkpoints sent</p>
                    </CardContent>
                </Card>
                <Card>
                    <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                        <CardTitle className="text-sm font-medium">Response Rate</CardTitle>
                        <CheckCircle2 className="h-4 w-4 text-green-500" />
                    </CardHeader>
                    <CardContent>
                        <div className="text-2xl font-bold">
                            {(stats?.total ?? 0) > 0 ? Math.round(((stats?.responded ?? 0) / stats.total) * 100) : 0}%
                        </div>
                        <p className="text-xs text-muted-foreground">{stats?.responded || 0} responses received</p>
                    </CardContent>
                </Card>
                <Card>
                    <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                        <CardTitle className="text-sm font-medium">Pending</CardTitle>
                        <Clock className="h-4 w-4 text-blue-500" />
                    </CardHeader>
                    <CardContent>
                        <div className="text-2xl font-bold">{stats?.active_pending || 0}</div>
                        <p className="text-xs text-muted-foreground">Awaiting response (active)</p>
                    </CardContent>
                </Card>
                <Card>
                    <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                        <CardTitle className="text-sm font-medium">Escalations</CardTitle>
                        <AlertTriangle className="h-4 w-4 text-red-500" />
                    </CardHeader>
                    <CardContent>
                        <div className="text-2xl font-bold">{stats?.by_type?.escalation || 0}</div>
                        <p className="text-xs text-muted-foreground">Triggered by intervention</p>
                    </CardContent>
                </Card>
            </div>

            {/* Filters & Content */}
            <div className="bg-white border rounded-lg shadow-sm">
                <div className="p-4 border-b flex flex-col sm:flex-row items-center justify-between gap-4 bg-gray-50/50">
                    <div className="flex items-center gap-2 w-full sm:w-auto">
                        <Filter className="w-4 h-4 text-gray-500" />
                        <select
                            className="bg-white border border-gray-300 text-gray-900 text-sm rounded-md focus:ring-blue-500 focus:border-blue-500 block w-full p-2"
                            value={eventTypeFilter}
                            onChange={(e) => setEventTypeFilter(e.target.value)}
                        >
                            <option value="all">All Events</option>
                            <option value="first_reply">First Reply</option>
                            <option value="escalation">Escalation</option>
                            <option value="ai_toggle">AI Toggle</option>
                            <option value="onboarding_48h">Onboarding (48h)</option>
                        </select>
                    </div>
                    <Button variant="outline" size="sm" onClick={() => loadData()} className="ml-auto">
                        Refresh Data
                    </Button>
                </div>

                <div className="overflow-x-auto">
                    <table className="w-full text-sm text-left">
                        <thead className="text-xs text-gray-500 uppercase bg-gray-50 border-b">
                            <tr>
                                <th className="px-6 py-3 font-medium">Date</th>
                                <th className="px-6 py-3 font-medium">Tenant</th>
                                <th className="px-6 py-3 font-medium">Event Type</th>
                                <th className="px-6 py-3 font-medium">Feedback</th>
                                <th className="px-6 py-3 font-medium">Trust Score</th>
                                <th className="px-6 py-3 font-medium text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-gray-100">
                            {loading ? (
                                <tr>
                                    <td colSpan={6} className="px-6 py-8 text-center text-gray-500">
                                        Loading checkpoint data...
                                    </td>
                                </tr>
                            ) : responses.length === 0 ? (
                                <tr>
                                    <td colSpan={6} className="px-6 py-8 text-center text-gray-500">
                                        No checkpoint responses found matching filters.
                                    </td>
                                </tr>
                            ) : (
                                responses.map((r, idx) => (
                                    <tr key={r.id || idx} className="hover:bg-gray-50/50 transition-colors">
                                        <td className="px-6 py-4 whitespace-nowrap text-gray-500">
                                            {safeFormatDate(r.created_at || r.CreatedAt)}
                                        </td>
                                        <td className="px-6 py-4 font-medium text-gray-900">
                                            {r.tenant_name || r.TenantName || 'Unknown Tenant'}
                                        </td>
                                        <td className="px-6 py-4">
                                            {getEventBadge(r.event_type || r.EventType)}
                                        </td>
                                        <td className="px-6 py-4 max-w-xs truncate">
                                            <div className="font-medium text-gray-900">{r.selected_reason || r.SelectedReason || 'No reason selected'}</div>
                                            {/* Access free text with fallback */}
                                            {(r.free_text || r.FreeText) && (
                                                <div className="text-xs text-gray-500 mt-0.5 truncate" title={r.free_text || r.FreeText}>
                                                    "{r.free_text || r.FreeText}"
                                                </div>
                                            )}
                                        </td>
                                        <td className="px-6 py-4">
                                            {renderTrustScore(r.trust_score ?? r.TrustScore)}
                                        </td>
                                        <td className="px-6 py-4 text-right">
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                onClick={() => {
                                                    navigator.clipboard.writeText(r.tenant_id)
                                                    setResultModal({
                                                        open: true,
                                                        title: 'Copied',
                                                        message: 'Tenant ID copied to clipboard.',
                                                        type: 'success'
                                                    })
                                                }}
                                                title="Copy Tenant ID"
                                            >
                                                <Copy className="h-4 w-4 text-gray-400" />
                                            </Button>
                                        </td>
                                    </tr>
                                ))
                            )}
                        </tbody>
                    </table>
                </div>
            </div>
        </div>
    )
}
