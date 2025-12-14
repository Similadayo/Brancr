import { useEscalations } from './useEscalations';

export default function EscalationsPage() {
  const { escalations, loading, error } = useEscalations();

  return (
    <div className="escalationsPage">
      <div className="pageTitle">Escalations</div>
      {loading && <div>Loading…</div>}
      {error && <div className="error">{error}</div>}
      {!loading && !error && (
        <table className="escalationsTable">
          <thead>
            <tr>
              <th>ID</th>
              <th>Tenant</th>
              <th>Customer</th>
              <th>Platform</th>
              <th>Intent</th>
              <th>Tone</th>
              <th>Preview</th>
              <th>Created</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {escalations.length === 0 ? (
              <tr>
                <td colSpan={10} style={{ textAlign: 'center', color: '#888', padding: 40 }}>
                  No pending escalations
                </td>
              </tr>
            ) : (
              escalations.map((e) => (
                <tr key={e.InteractionID}>
                  <td>{e.InteractionID}</td>
                  <td>{e.TenantName}</td>
                  <td>{e.CustomerName}</td>
                  <td>{e.Platform}</td>
                  <td>{e.Intent}</td>
                  <td>{e.Tone}</td>
                  <td>{e.ContentPreview}</td>
                  <td>{e.CreatedAt}</td>
                  <td>{e.Status}</td>
                  <td>
                    {/* TODO: Add resolve/ignore actions with RBAC checks */}
                    <button disabled>Resolve</button>
                    <button disabled style={{ marginLeft: 6 }}>Ignore</button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      )}
    </div>
  );
}
