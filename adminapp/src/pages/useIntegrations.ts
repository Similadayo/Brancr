import { useEffect, useState } from 'react';

export type IntegrationStatus = {
  name: string;
  status: string;
  last_error: string | null;
  last_checked: string;
};

export function useIntegrations() {
  const [integrations, setIntegrations] = useState<IntegrationStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    fetch('/admin/api/integrations', { headers: { Accept: 'application/json' } })
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch integrations');
        return r.json();
      })
      .then((data) => {
        setIntegrations(data.integrations || []);
        setError(null);
      })
      .catch((e) => {
        setError(e.message);
        setIntegrations([]);
      })
      .finally(() => setLoading(false));
  }, []);

  return { integrations, loading, error };
}
