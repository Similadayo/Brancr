import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Pagination } from '@/components/ui/pagination'
import { format } from 'date-fns'
import {
  Search,
  CheckCircle2,
  XCircle,
  AlertTriangle,
  ExternalLink,
  Instagram,
  Facebook,
  MessageSquare,
  Music
} from 'lucide-react'

interface SocialAccount {
  id: string
  tenant_id: string
  tenant_name: string
  platform: string
  username: string
  connected: boolean
  expires_at?: string
  expires_soon: boolean
  mode: string
  ai_mode: string
  created_at: string
}

interface SocialAccountsResponse {
  total: number
  items: SocialAccount[]
  limit: number
  offset: number
}

const platformIcons: Record<string, React.ReactNode> = {
  instagram: <Instagram className="h-4 w-4" />,
  facebook: <Facebook className="h-4 w-4" />,
  whatsapp: <MessageSquare className="h-4 w-4" />,
  tiktok: <Music className="h-4 w-4" />,
}

export default function SocialAccountsPage() {
  const [search, setSearch] = useState('')
  const [platformFilter, setPlatformFilter] = useState('')
  const [connectedOnly, setConnectedOnly] = useState(false)
  const [limit] = useState(50)
  const [offset, setOffset] = useState(0)

  const { data, isLoading } = useQuery<SocialAccountsResponse>({
    queryKey: ['social-accounts', platformFilter, connectedOnly, search, limit, offset],
    queryFn: () => api.getSocialAccounts(
      platformFilter || undefined,
      connectedOnly || undefined,
      search || undefined,
      limit,
      offset
    ),
  })

  // Group accounts by tenant
  const groupedByTenant = data?.items?.reduce((acc, account) => {
    if (!acc[account.tenant_id]) {
      acc[account.tenant_id] = {
        tenant_id: account.tenant_id,
        tenant_name: account.tenant_name,
        accounts: [],
      }
    }
    acc[account.tenant_id].accounts.push(account)
    return acc
  }, {} as Record<string, { tenant_id: string; tenant_name: string; accounts: SocialAccount[] }>)

  const tenantGroups = groupedByTenant ? Object.values(groupedByTenant) : []

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    setOffset(0)
  }

  const getPlatformIcon = (platform: string) => {
    return platformIcons[platform.toLowerCase()] || <ExternalLink className="h-4 w-4" />
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Social Accounts</h1>
        <p className="text-muted-foreground">
          Manage connected social media accounts including WhatsApp (Gupshup) ({data?.total || 0} total)
        </p>
      </div>

      {/* Filters */}
      <div className="flex flex-col sm:flex-row gap-4">
        <form onSubmit={handleSearch} className="flex-1">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-gray-400" />
            <Input
              type="text"
              placeholder="Search by tenant name or username..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-10"
            />
          </div>
        </form>
        <div className="flex gap-2">
          <Button
            variant={platformFilter === '' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              setPlatformFilter('')
              setOffset(0)
            }}
          >
            All Platforms
          </Button>
          <Button
            variant={platformFilter === 'instagram' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              setPlatformFilter('instagram')
              setOffset(0)
            }}
          >
            Instagram
          </Button>
          <Button
            variant={platformFilter === 'facebook' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              setPlatformFilter('facebook')
              setOffset(0)
            }}
          >
            Facebook
          </Button>
          <Button
            variant={platformFilter === 'whatsapp' ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              setPlatformFilter('whatsapp')
              setOffset(0)
            }}
          >
            WhatsApp
          </Button>
          <Button
            variant={connectedOnly ? 'default' : 'outline'}
            size="sm"
            onClick={() => {
              setConnectedOnly(!connectedOnly)
              setOffset(0)
            }}
          >
            Connected Only
          </Button>
        </div>
      </div>

      {/* Table */}
      {isLoading ? (
        <div className="flex items-center justify-center py-12">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      ) : (
        <div className="bg-white rounded-lg border border-gray-200 shadow-sm overflow-hidden">
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Tenant
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Platform
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Username
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Status
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Mode
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Expires
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Created
                  </th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody className="bg-white divide-y divide-gray-200">
                {tenantGroups.length === 0 ? (
                  <tr>
                    <td colSpan={8} className="px-6 py-12 text-center text-sm text-gray-500">
                      No social accounts found
                    </td>
                  </tr>
                ) : (
                  tenantGroups.map((group) => (
                    <>
                      {/* Tenant Header Row */}
                      <tr key={`tenant-${group.tenant_id}`} className="bg-gray-100">
                        <td colSpan={8} className="px-6 py-3">
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-2">
                              <span className="text-sm font-semibold text-gray-900">
                                {group.tenant_name}
                              </span>
                              <Badge variant="outline" className="text-xs">
                                {group.accounts.length} account{group.accounts.length !== 1 ? 's' : ''}
                              </Badge>
                            </div>
                            <Link to={`/admin/app/tenants/${group.tenant_id}`}>
                              <Button variant="ghost" size="sm">
                                <ExternalLink className="h-3 w-3 mr-1" />
                                View Tenant
                              </Button>
                            </Link>
                          </div>
                        </td>
                      </tr>
                      {/* Account Rows */}
                      {group.accounts.map((account) => (
                        <tr key={account.id} className="hover:bg-gray-50">
                          <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                            {/* Empty - tenant name is in header */}
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap">
                            <div className="flex items-center gap-2">
                              <span className="text-gray-700">
                                {getPlatformIcon(account.platform)}
                              </span>
                              <span className="text-sm font-medium text-gray-900 capitalize">
                                {account.platform}
                              </span>
                            </div>
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">
                            {account.username || 'N/A'}
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap">
                            {account.connected ? (
                              <Badge variant="default" className="flex items-center gap-1 w-fit">
                                <CheckCircle2 className="h-3 w-3" />
                                Connected
                              </Badge>
                            ) : (
                              <Badge variant="destructive" className="flex items-center gap-1 w-fit">
                                <XCircle className="h-3 w-3" />
                                Disconnected
                              </Badge>
                            )}
                            {account.expires_soon && (
                              <Badge variant="outline" className="ml-2 flex items-center gap-1 w-fit">
                                <AlertTriangle className="h-3 w-3" />
                                Expires Soon
                              </Badge>
                            )}
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                            <div className="flex flex-col gap-1">
                              <Badge variant="outline" className="text-xs w-fit">
                                {account.mode || 'N/A'}
                              </Badge>
                              <Badge variant="outline" className="text-xs w-fit">
                                AI: {account.ai_mode || 'N/A'}
                              </Badge>
                            </div>
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                            {account.expires_at ? (
                              <span className={account.expires_soon ? 'text-orange-600 font-medium' : ''}>
                                {format(new Date(account.expires_at), 'MMM d, yyyy')}
                              </span>
                            ) : (
                              <span className="text-gray-400">Never</span>
                            )}
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                            {format(new Date(account.created_at), 'MMM d, yyyy')}
                          </td>
                          <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                            <Link to={`/admin/app/tenants/${account.tenant_id}`}>
                              <Button variant="ghost" size="sm">
                                View
                              </Button>
                            </Link>
                          </td>
                        </tr>
                      ))}
                    </>
                  ))
                )}
              </tbody>
            </table>
          </div>
          {data && (
            <Pagination
              total={data.total}
              limit={limit}
              offset={offset}
              onPageChange={setOffset}
            />
          )}
        </div>
      )}
    </div>
  )
}
