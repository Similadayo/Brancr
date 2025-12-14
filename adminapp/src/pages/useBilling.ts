import { useEffect, useState } from 'react';

export type BillingItem = {
  tenant: string;
  usage: number;
  quota: number;
  payment_status: string;
  escalated: boolean;
  last_payment: string;
};

export function useBilling() {
  const [items, setItems] = useState<BillingItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    fetch('/admin/api/billing', { headers: { Accept: 'application/json' } })
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch billing');
        return r.json();
      })
      .then((data) => {
        setItems(data.billing || []);
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
