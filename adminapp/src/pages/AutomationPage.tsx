import { useAutomationJobs } from './useAutomationJobs';

export default function AutomationPage() {
  const { jobs, loading, error } = useAutomationJobs();

  return (
    <div className="automationPage">
      <div className="pageHeader">
        <div className="pageTitle">Automation Monitoring</div>
      </div>
      {loading ? (
        <div>Loading…</div>
      ) : error ? (
        <div className="error">{error}</div>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>Type</th>
              <th>Status</th>
              <th>Last Run</th>
              <th>Next Run</th>
              <th>Error</th>
            </tr>
          </thead>
          <tbody>
            {jobs.map((j) => (
              <tr key={j.id}>
                <td>{j.type}</td>
                <td>{j.status}</td>
                <td>{new Date(j.last_run).toLocaleString()}</td>
                <td>{new Date(j.next_run).toLocaleString()}</td>
                <td>{j.error || ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
