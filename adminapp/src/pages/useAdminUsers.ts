import { useEffect, useState } from 'react';

export type AdminUser = {
  id: string;
  email: string;
  role: string;
  active: boolean;
  created_at: string;
};

export function useAdminUsers() {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    fetch('/admin/api/admin-users', { headers: { Accept: 'application/json' } })
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch admin users');
        return r.json();
      })
      .then((data) => {
        setUsers(data.users || []);
        setError(null);
      })
      .catch((e) => {
        setError(e.message);
        setUsers([]);
      })
      .finally(() => setLoading(false));
  }, []);

  return { users, loading, error };
}
