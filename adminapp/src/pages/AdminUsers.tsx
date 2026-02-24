import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type AdminUser } from '@/lib/api'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Plus, Edit2, Save, X, CheckCircle2, XCircle } from 'lucide-react'

const ROLES = [
  'SuperAdmin',
  'SupportAdmin',
  'PlatformAdmin',
  'BillingAdmin',
  'ComplianceAdmin',
  'OperationsSupport',
]

export default function AdminUsers() {
  const queryClient = useQueryClient()
  const [editingId, setEditingId] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [formData, setFormData] = useState({
    email: '',
    role: 'SupportAdmin',
    password: '',
    active: true,
  })

  const { data: users, isLoading } = useQuery({
    queryKey: ['admin-users'],
    queryFn: () => api.getAdminUsers(),
  })

  const createMutation = useMutation({
    mutationFn: (data: { email: string; role: string; password: string }) =>
      api.createAdminUser(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin-users'] })
      setShowCreate(false)
      setFormData({ email: '', role: 'SupportAdmin', password: '', active: true })
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: any }) =>
      api.updateAdminUser(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin-users'] })
      setEditingId(null)
    },
  })

  const handleCreate = () => {
    if (!formData.email || !formData.password) {
      alert('Email and password are required')
      return
    }
    createMutation.mutate({
      email: formData.email,
      role: formData.role,
      password: formData.password,
    })
  }

  const handleUpdate = (id: string, updates: any) => {
    updateMutation.mutate({ id, data: updates })
  }

  if (isLoading) {
    return <div>Loading...</div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">Admin Users</h1>
          <p className="text-muted-foreground mt-1">Manage admin accounts and roles</p>
        </div>
        <Button onClick={() => setShowCreate(!showCreate)}>
          <Plus className="h-4 w-4 mr-2" />
          Add User
        </Button>
      </div>

      {/* Create form */}
      {showCreate && (
        <Card>
          <CardHeader>
            <CardTitle>Create New Admin User</CardTitle>
            <CardDescription>Add a new admin user with a specific role</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
              <div>
                <label className="block text-sm font-medium mb-1">Email</label>
                <input
                  type="email"
                  className="w-full px-3 py-2 border rounded-lg"
                  value={formData.email}
                  onChange={(e) => setFormData({ ...formData, email: e.target.value })}
                  placeholder="admin@company.com"
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">Role</label>
                <select
                  className="w-full px-3 py-2 border rounded-lg"
                  value={formData.role}
                  onChange={(e) => setFormData({ ...formData, role: e.target.value })}
                >
                  {ROLES.map((role) => (
                    <option key={role} value={role}>
                      {role}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">Password</label>
                <input
                  type="password"
                  className="w-full px-3 py-2 border rounded-lg"
                  value={formData.password}
                  onChange={(e) => setFormData({ ...formData, password: e.target.value })}
                  placeholder="min 8 characters"
                />
              </div>
              <div className="flex items-end gap-2">
                <Button onClick={handleCreate} disabled={createMutation.isPending}>
                  Create
                </Button>
                <Button
                  variant="outline"
                  onClick={() => {
                    setShowCreate(false)
                    setFormData({ email: '', role: 'SupportAdmin', password: '', active: true })
                  }}
                >
                  Cancel
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Users list */}
      <Card>
        <CardHeader>
          <CardTitle>Admin Users</CardTitle>
          <CardDescription>List of all admin users and their roles</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b">
                  <th className="text-left py-3 px-4 font-semibold">Email</th>
                  <th className="text-left py-3 px-4 font-semibold">Role</th>
                  <th className="text-left py-3 px-4 font-semibold">Status</th>
                  <th className="text-left py-3 px-4 font-semibold">Last Login</th>
                  <th className="text-left py-3 px-4 font-semibold">Actions</th>
                </tr>
              </thead>
              <tbody>
                {users?.map((user) => (
                  <UserRow
                    key={user.id}
                    user={user}
                    isEditing={editingId === user.id}
                    onEdit={() => setEditingId(user.id)}
                    onCancel={() => setEditingId(null)}
                    onSave={handleUpdate}
                  />
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function UserRow({
  user,
  isEditing,
  onEdit,
  onCancel,
  onSave,
}: {
  user: AdminUser
  isEditing: boolean
  onEdit: () => void
  onCancel: () => void
  onSave: (id: string, updates: any) => void
}) {
  const [updates, setUpdates] = useState({
    role: user.role,
    active: user.active,
    password: '',
  })

  if (isEditing) {
    return (
      <tr className="border-b">
        <td className="py-3 px-4">{user.email}</td>
        <td className="py-3 px-4">
          <select
            className="w-full px-2 py-1 border rounded"
            value={updates.role}
            onChange={(e) => setUpdates({ ...updates, role: e.target.value })}
          >
            {ROLES.map((role) => (
              <option key={role} value={role}>
                {role}
              </option>
            ))}
          </select>
        </td>
        <td className="py-3 px-4">
          <select
            className="w-full px-2 py-1 border rounded"
            value={updates.active ? 'true' : 'false'}
            onChange={(e) => setUpdates({ ...updates, active: e.target.value === 'true' })}
          >
            <option value="true">Active</option>
            <option value="false">Inactive</option>
          </select>
        </td>
        <td className="py-3 px-4 text-sm text-gray-500">
          {user.last_login ? new Date(user.last_login).toLocaleDateString() : 'Never'}
        </td>
        <td className="py-3 px-4">
          <div className="flex gap-2">
            <Button
              size="sm"
              onClick={() => onSave(user.id, updates)}
              className="h-8"
            >
              <Save className="h-3 w-3 mr-1" />
              Save
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={onCancel}
              className="h-8"
            >
              <X className="h-3 w-3 mr-1" />
              Cancel
            </Button>
          </div>
        </td>
      </tr>
    )
  }

  return (
    <tr className="border-b hover:bg-gray-50">
      <td className="py-3 px-4">{user.email}</td>
      <td className="py-3 px-4">
        <span className="px-2 py-1 bg-blue-100 text-blue-800 rounded text-sm font-medium">
          {user.role}
        </span>
      </td>
      <td className="py-3 px-4">
        {user.active ? (
          <span className="flex items-center gap-1 text-green-600">
            <CheckCircle2 className="h-4 w-4" />
            Active
          </span>
        ) : (
          <span className="flex items-center gap-1 text-gray-400">
            <XCircle className="h-4 w-4" />
            Inactive
          </span>
        )}
      </td>
      <td className="py-3 px-4 text-sm text-gray-500">
        {user.last_login ? new Date(user.last_login).toLocaleDateString() : 'Never'}
      </td>
      <td className="py-3 px-4">
        <Button size="sm" variant="outline" onClick={onEdit} className="h-8">
          <Edit2 className="h-3 w-3 mr-1" />
          Edit
        </Button>
      </td>
    </tr>
  )
}

