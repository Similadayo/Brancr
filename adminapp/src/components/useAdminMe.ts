import { useEffect, useState } from 'react';

export type AdminMe = {
  email: string;
  role: string;
};

export function useAdminMe() {
  const [me, setMe] = useState<AdminMe | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    fetch('/admin/api/me')
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch admin info');
        return r.json();
      })
      .then((data) => {
        setMe(data);
        setError(null);
      })
      .catch((e) => {
        setError(e.message);
        setMe(null);
      })
      .finally(() => setLoading(false));
  }, []);

  return { me, loading, error };
}
