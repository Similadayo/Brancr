import { useEffect, useState } from 'react';

export type OnboardingItem = {
  id: string;
  tenant: string;
  state: string;
  error: string | null;
  escalated: boolean;
  created_at: string;
  updated_at: string;
};

export function useOnboarding() {
  const [items, setItems] = useState<OnboardingItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    fetch('/admin/api/onboarding', { headers: { Accept: 'application/json' } })
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch onboarding');
        return r.json();
      })
      .then((data) => {
        setItems(data.onboarding || []);
        setError(null);
      })
      .catch((e) => {
        setError(e.message);
        setItems([]);
      })
      .finally(() => setLoading(false));
  }, []);

  return { items, loading, error };
}
