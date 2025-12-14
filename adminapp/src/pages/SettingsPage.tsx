import { useAdminUsers } from './useAdminUsers';

export default function SettingsPage() {
  const { users, loading, error } = useAdminUsers();

  return (
    <div className="settingsPage">
      <div className="pageHeader">
        <div className="pageTitle">Admin User Management</div>
      </div>
      {loading ? (
        <div>Loading…</div>
      ) : error ? (
        <div className="error">{error}</div>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>Email</th>
              <th>Role</th>
              <th>Status</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id}>
                <td>{u.email}</td>
                <td>{u.role}</td>
                <td>{u.active ? 'Active' : 'Inactive'}</td>
                <td>{new Date(u.created_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
