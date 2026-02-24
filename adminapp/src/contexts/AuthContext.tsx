import { createContext, useContext, useEffect, useState, ReactNode } from 'react'
import { api } from '@/lib/api'

interface AdminUser {
  email: string
  role: string
}

interface AuthContextType {
  user: AdminUser | null
  loading: boolean
  refetch: () => void
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  loading: true,
  refetch: () => { },
})

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AdminUser | null>(null)
  const [loading, setLoading] = useState(true)

  const fetchUser = async () => {
    try {
      const data = await api.getMe()
      setUser(data)
    } catch (error) {
      console.error('Failed to fetch user:', error)
      setUser(null)
      // Redirect to login on 401
      if (error instanceof Error && error.message.includes('401')) {
        window.location.href = '/admin/login'
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchUser()
  }, [])

  return (
    <AuthContext.Provider value={{ user, loading, refetch: fetchUser }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}

