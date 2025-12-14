import { useState } from 'react';
import { useOnboarding, OnboardingItem } from './useOnboarding';

export default function OnboardingPage() {
  const { items, loading, error } = useOnboarding();
  const [search, setSearch] = useState('');

  const filtered = items.filter((i) =>
    i.tenant.toLowerCase().includes(search.toLowerCase()) ||
    i.id.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="onboardingPage">
      <div className="pageHeader">
        <div className="pageTitle">Onboarding Monitoring</div>
        <input
          className="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search onboarding…"
        />
      </div>
      {loading ? (
        <div>Loading…</div>
      ) : error ? (
        <div className="error">{error}</div>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>Tenant</th>
              <th>State</th>
              <th>Error</th>
              <th>Escalated</th>
              <th>Created</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((i) => (
              <tr key={i.id}>
                <td>{i.tenant}</td>
                <td>{i.state}</td>
                <td>{i.error || ''}</td>
                <td>{i.escalated ? 'Yes' : 'No'}</td>
                <td>{new Date(i.created_at).toLocaleString()}</td>
                <td>{new Date(i.updated_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
