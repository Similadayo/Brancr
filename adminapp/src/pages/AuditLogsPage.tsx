import { useState } from 'react';
import { useAuditLogs } from './useAuditLogs';

export default function AuditLogsPage() {
  const { logs, loading, error } = useAuditLogs();
  const [filter, setFilter] = useState('');

  const filtered = logs.filter((l) =>
    l.admin.toLowerCase().includes(filter.toLowerCase()) ||
    l.action.toLowerCase().includes(filter.toLowerCase()) ||
    l.target.toLowerCase().includes(filter.toLowerCase())
  );

  return (
    <div className="auditLogsPage">
      <div className="pageHeader">
        <div className="pageTitle">Audit Logs</div>
        <input
          className="search"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter by admin, action, or target…"
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
              <th>Admin</th>
              <th>Action</th>
              <th>Target</th>
              <th>Details</th>
              <th>Timestamp</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((l) => (
              <tr key={l.id}>
                <td>{l.admin}</td>
                <td>{l.action}</td>
                <td>{l.target}</td>
                <td>{l.details}</td>
                <td>{new Date(l.timestamp).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
