import { useEffect, useMemo, useState } from 'react';

export type Tenant = {
  id: string;
  name: string;
  status: string;
  quota: number;
  created_at: string;
  escalated: boolean;
};

export function useTenants() {
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    fetch('/admin/api/tenants', { headers: { Accept: 'application/json' } })
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch tenants');
        return r.json();
      })
      .then((data) => {
        setTenants(data.tenants || []);
        setError(null);
      })
      .catch((e) => {
        setError(e.message);
        setTenants([]);
      })
      .finally(() => setLoading(false));
  }, []);

  return { tenants, loading, error };
}
