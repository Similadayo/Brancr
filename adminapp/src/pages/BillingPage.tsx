import { useBilling } from './useBilling';

export default function BillingPage() {
  const { items, loading, error } = useBilling();

  return (
    <div className="billingPage">
      <div className="pageHeader">
        <div className="pageTitle">Billing &amp; Usage</div>
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
              <th>Usage</th>
              <th>Quota</th>
              <th>Payment Status</th>
              <th>Escalated</th>
              <th>Last Payment</th>
            </tr>
          </thead>
          <tbody>
            {items.map((i) => (
              <tr key={i.tenant}>
                <td>{i.tenant}</td>
                <td>{i.usage}</td>
                <td>{i.quota}</td>
                <td>{i.payment_status}</td>
                <td>{i.escalated ? 'Yes' : 'No'}</td>
                <td>{new Date(i.last_payment).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
