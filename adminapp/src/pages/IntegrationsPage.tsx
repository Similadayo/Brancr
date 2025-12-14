import { useIntegrations } from './useIntegrations';

export default function IntegrationsPage() {
  const { integrations, loading, error } = useIntegrations();

  return (
    <div className="integrationsPage">
      <div className="pageHeader">
        <div className="pageTitle">Integrations Health</div>
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
              <th>Last Error</th>
              <th>Last Checked</th>
            </tr>
          </thead>
          <tbody>
            {integrations.map((i) => (
              <tr key={i.name}>
                <td>{i.name}</td>
                <td>{i.status}</td>
                <td>{i.last_error || ''}</td>
                <td>{new Date(i.last_checked).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
