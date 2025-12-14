import { useEffect, useState } from 'react';

export type AutomationJob = {
  id: string;
  type: string;
  status: string;
  last_run: string;
  next_run: string;
  error: string | null;
};

export function useAutomationJobs() {
  const [jobs, setJobs] = useState<AutomationJob[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    fetch('/admin/api/automation', { headers: { Accept: 'application/json' } })
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch automation jobs');
        return r.json();
      })
      .then((data) => {
        setJobs(data.jobs || []);
        setError(null);
      })
      .catch((e) => {
        setError(e.message);
        setJobs([]);
      })
      .finally(() => setLoading(false));
  }, []);

  return { jobs, loading, error };
}
