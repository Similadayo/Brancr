import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, AlertTemplate } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { format } from 'date-fns'
import { AlertCircle, Info, AlertTriangle, Edit2, RotateCcw, Save, X, MessageSquare } from 'lucide-react'
import { useState } from 'react'

type TemplateType = 'all' | 'system' | 'billing' | 'security' | 'feature' | 'quota'

interface EditingTemplate {
    id: string
    title: string
    message: string
    action_url: string
    action_label: string
    is_active: boolean
}

export default function AlertTemplatesPage() {
    const [filterType, setFilterType] = useState<TemplateType>('all')
    const [editingTemplate, setEditingTemplate] = useState<EditingTemplate | null>(null)
    const queryClient = useQueryClient()

    const { data: templates, isLoading } = useQuery({
        queryKey: ['alert-templates', filterType],
        queryFn: () => api.getAlertTemplates(filterType === 'all' ? undefined : filterType),
    })

    const updateMutation = useMutation({
        mutationFn: (data: EditingTemplate) => api.updateAlertTemplate(data.id, {
            title: data.title,
            message: data.message,
            action_url: data.action_url,
            action_label: data.action_label,
            is_active: data.is_active,
        }),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['alert-templates'] })
            setEditingTemplate(null)
        },
    })

    const resetMutation = useMutation({
        mutationFn: (key: string) => api.resetAlertTemplate(key),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['alert-templates'] })
        },
    })

    const getSeverityBadge = (severity: string) => {
        const variants: Record<string, { variant: 'default' | 'secondary' | 'destructive' | 'outline'; icon: React.ReactNode }> = {
            info: { variant: 'default', icon: <Info className="h-3 w-3" /> },
            warning: { variant: 'secondary', icon: <AlertTriangle className="h-3 w-3" /> },
            critical: { variant: 'destructive', icon: <AlertCircle className="h-3 w-3" /> },
        }
        const config = variants[severity] || variants.info
        return (
            <Badge variant={config.variant} className="flex items-center gap-1">
                {config.icon}
                {severity}
            </Badge>
        )
    }

    const getTypeBadge = (type: string) => {
        const colors: Record<string, string> = {
            system: 'bg-blue-100 text-blue-800',
            billing: 'bg-green-100 text-green-800',
            security: 'bg-red-100 text-red-800',
            feature: 'bg-purple-100 text-purple-800',
            quota: 'bg-orange-100 text-orange-800',
        }
        return (
            <span className={`px-2 py-1 text-xs rounded ${colors[type] || 'bg-gray-100 text-gray-800'}`}>
                {type}
            </span>
        )
    }

    const startEditing = (template: AlertTemplate) => {
        setEditingTemplate({
            id: template.id,
            title: template.title,
            message: template.message,
            action_url: template.action_url || '',
            action_label: template.action_label || '',
            is_active: template.is_active,
        })
    }

    const handleSave = () => {
        if (editingTemplate) {
            updateMutation.mutate(editingTemplate)
        }
    }

    const handleReset = (key: string) => {
        if (confirm('Are you sure you want to reset this template to its default values?')) {
            resetMutation.mutate(key)
        }
    }

    const typeOptions: TemplateType[] = ['all', 'system', 'billing', 'security', 'feature', 'quota']

    return (
        <div className="space-y-6">
            <div className="flex items-center justify-between">
                <div>
                    <h1 className="text-3xl font-bold tracking-tight">Alert Templates</h1>
                    <p className="text-muted-foreground">
                        Edit automated alert messages sent to tenants
                    </p>
                </div>
                <div className="flex items-center gap-2">
                    <MessageSquare className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm text-muted-foreground">
                        {templates?.length || 0} templates
                    </span>
                </div>
            </div>

            {/* Filter */}
            <Card>
                <CardHeader className="pb-3">
                    <CardTitle className="text-lg">Filter by Type</CardTitle>
                </CardHeader>
                <CardContent>
                    <div className="flex flex-wrap gap-2">
                        {typeOptions.map((type) => (
                            <Button
                                key={type}
                                variant={filterType === type ? 'default' : 'outline'}
                                size="sm"
                                onClick={() => setFilterType(type)}
                                className="capitalize"
                            >
                                {type === 'all' ? 'All Types' : type}
                            </Button>
                        ))}
                    </div>
                </CardContent>
            </Card>

            {/* Templates */}
            <div className="space-y-4">
                {isLoading ? (
                    <Card>
                        <CardContent className="py-12 text-center text-muted-foreground">
                            Loading templates...
                        </CardContent>
                    </Card>
                ) : templates?.length === 0 ? (
                    <Card>
                        <CardContent className="py-12 text-center text-muted-foreground">
                            No templates found
                        </CardContent>
                    </Card>
                ) : (
                    templates?.map((template: AlertTemplate) => (
                        <Card key={template.id} className={!template.is_active ? 'opacity-60' : ''}>
                            <CardHeader className="pb-2">
                                <div className="flex items-start justify-between">
                                    <div className="space-y-1">
                                        <CardTitle className="text-lg flex items-center gap-2">
                                            {template.name}
                                            {!template.is_active && (
                                                <Badge variant="outline" className="text-xs">Disabled</Badge>
                                            )}
                                        </CardTitle>
                                        <CardDescription className="flex items-center gap-2">
                                            <code className="text-xs bg-muted px-1 py-0.5 rounded">{template.key}</code>
                                            <span>•</span>
                                            <span>{template.description}</span>
                                        </CardDescription>
                                    </div>
                                    <div className="flex items-center gap-2">
                                        {getTypeBadge(template.type)}
                                        {getSeverityBadge(template.severity)}
                                    </div>
                                </div>
                            </CardHeader>
                            <CardContent>
                                {editingTemplate?.id === template.id ? (
                                    /* Editing Mode */
                                    <div className="space-y-4">
                                        <div>
                                            <label className="text-sm font-medium mb-1 block">Title</label>
                                            <Input
                                                value={editingTemplate.title}
                                                onChange={(e) => setEditingTemplate({
                                                    ...editingTemplate,
                                                    title: e.target.value,
                                                })}
                                                placeholder="Alert title (supports {{.TenantName}} placeholders)"
                                            />
                                            <p className="text-xs text-muted-foreground mt-1">
                                                Placeholders: {'{{.TenantName}}'}, {'{{.DaysLeft}}'}, {'{{.PlanName}}'}, etc.
                                            </p>
                                        </div>
                                        <div>
                                            <label className="text-sm font-medium mb-1 block">Message</label>
                                            <textarea
                                                className="w-full min-h-[100px] p-2 border rounded text-sm"
                                                value={editingTemplate.message}
                                                onChange={(e) => setEditingTemplate({
                                                    ...editingTemplate,
                                                    message: e.target.value,
                                                })}
                                                placeholder="Alert message body"
                                            />
                                        </div>
                                        <div className="grid grid-cols-2 gap-4">
                                            <div>
                                                <label className="text-sm font-medium mb-1 block">Action URL</label>
                                                <Input
                                                    value={editingTemplate.action_url}
                                                    onChange={(e) => setEditingTemplate({
                                                        ...editingTemplate,
                                                        action_url: e.target.value,
                                                    })}
                                                    placeholder="/app/dashboard"
                                                />
                                            </div>
                                            <div>
                                                <label className="text-sm font-medium mb-1 block">Action Label</label>
                                                <Input
                                                    value={editingTemplate.action_label}
                                                    onChange={(e) => setEditingTemplate({
                                                        ...editingTemplate,
                                                        action_label: e.target.value,
                                                    })}
                                                    placeholder="Go to Dashboard"
                                                />
                                            </div>
                                        </div>
                                        <div className="flex items-center gap-2">
                                            <input
                                                type="checkbox"
                                                id={`active-${template.id}`}
                                                checked={editingTemplate.is_active}
                                                onChange={(e) => setEditingTemplate({
                                                    ...editingTemplate,
                                                    is_active: e.target.checked,
                                                })}
                                                className="rounded"
                                            />
                                            <label htmlFor={`active-${template.id}`} className="text-sm">
                                                Send this alert (enabled)
                                            </label>
                                        </div>
                                        <div className="flex gap-2">
                                            <Button onClick={handleSave} disabled={updateMutation.isPending}>
                                                <Save className="h-4 w-4 mr-2" />
                                                {updateMutation.isPending ? 'Saving...' : 'Save Changes'}
                                            </Button>
                                            <Button variant="outline" onClick={() => setEditingTemplate(null)}>
                                                <X className="h-4 w-4 mr-2" />
                                                Cancel
                                            </Button>
                                        </div>
                                    </div>
                                ) : (
                                    /* View Mode */
                                    <div className="space-y-3">
                                        <div className="p-3 bg-muted/50 rounded">
                                            <div className="font-medium text-sm mb-1">{template.title}</div>
                                            <p className="text-sm text-muted-foreground whitespace-pre-wrap">{template.message}</p>
                                        </div>
                                        {template.action_url && (
                                            <div className="flex items-center gap-2 text-sm">
                                                <span className="text-muted-foreground">Action:</span>
                                                <Badge variant="outline">{template.action_label || 'Click here'}</Badge>
                                                <span className="text-muted-foreground">→</span>
                                                <code className="text-xs bg-muted px-1 py-0.5 rounded">{template.action_url}</code>
                                            </div>
                                        )}
                                        <div className="flex items-center justify-between text-xs text-muted-foreground">
                                            <span>Last updated: {(() => {
                                                try {
                                                    if (!template.updated_at) return 'Never'
                                                    const date = new Date(template.updated_at)
                                                    if (isNaN(date.getTime())) return 'Unknown'
                                                    return format(date, 'PPp')
                                                } catch {
                                                    return 'Unknown'
                                                }
                                            })()}</span>
                                            <div className="flex gap-2">
                                                {template.is_editable && (
                                                    <>
                                                        <Button
                                                            size="sm"
                                                            variant="outline"
                                                            onClick={() => startEditing(template)}
                                                        >
                                                            <Edit2 className="h-3 w-3 mr-1" />
                                                            Edit
                                                        </Button>
                                                        <Button
                                                            size="sm"
                                                            variant="ghost"
                                                            onClick={() => handleReset(template.key)}
                                                            disabled={resetMutation.isPending}
                                                        >
                                                            <RotateCcw className="h-3 w-3 mr-1" />
                                                            Reset
                                                        </Button>
                                                    </>
                                                )}
                                            </div>
                                        </div>
                                    </div>
                                )}
                            </CardContent>
                        </Card>
                    ))
                )}
            </div>
        </div>
    )
}
