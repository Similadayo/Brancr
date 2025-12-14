import { useEffect, useState } from 'react';

export type AuditLog = {
  id: string;
  admin: string;
  action: string;
  target: string;
  timestamp: string;
  details: string;
};

export function useAuditLogs() {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    fetch('/admin/api/audit', { headers: { Accept: 'application/json' } })
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch audit logs');
        return r.json();
      })
      .then((data) => {
        setLogs(data.logs || []);
        setError(null);
      })
      .catch((e) => {
        setError(e.message);
        setLogs([]);
      })
      .finally(() => setLoading(false));
  }, []);

  return { logs, loading, error };
}
