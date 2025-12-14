import { useEffect, useState } from 'react';

export interface Escalation {
  InteractionID: number;
  TenantID: number;
  TenantName: string;
  CustomerID: number;
  CustomerName: string;
  Platform: string;
  Intent: string;
  Tone: string;
  Content: string;
  ContentPreview: string;
  CreatedAt: string;
  Status: string;
}

export function useEscalations() {
  const [escalations, setEscalations] = useState<Escalation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    fetch('/admin/escalations', { credentials: 'include' })
      .then(async (res) => {
        if (!res.ok) throw new Error('Failed to fetch escalations');
        const html = await res.text();
        // TODO: Replace with real API when available
        // For now, parse from HTML or use mock data
        setEscalations([]);
        setLoading(false);
      })
      .catch((e) => {
        setError(e.message);
        setLoading(false);
      });
  }, []);

  return { escalations, loading, error };
}
