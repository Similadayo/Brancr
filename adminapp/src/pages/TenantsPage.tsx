import { useState } from 'react';
import { useTenants, Tenant } from './useTenants';

export default function TenantsPage() {
  const { tenants, loading, error } = useTenants();
  const [search, setSearch] = useState('');

  const filtered = tenants.filter((t) =>
    t.name.toLowerCase().includes(search.toLowerCase()) ||
    t.id.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="tenantsPage">
      <div className="pageHeader">
        <div className="pageTitle">Tenants</div>
        <input
          className="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search tenants…"
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
              <th>Name</th>
              <th>Status</th>
              <th>Quota</th>
              <th>Escalated</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((t) => (
              <tr key={t.id}>
                <td>{t.name}</td>
                <td>{t.status}</td>
                <td>{t.quota}</td>
                <td>{t.escalated ? 'Yes' : 'No'}</td>
                <td>{new Date(t.created_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
