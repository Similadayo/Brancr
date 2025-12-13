import { Navigate, Route, Routes } from 'react-router-dom';
import Shell from './components/Shell';
import OverviewPage from './pages/OverviewPage';
import PlaceholderPage from './pages/PlaceholderPage';

export default function App() {
  return (
    <Shell>
      <Routes>
        <Route path="/" element={<OverviewPage />} />
        <Route path="/tenants" element={<PlaceholderPage title="Tenant Management" />} />
        <Route path="/cases" element={<PlaceholderPage title="Support Cases" />} />
        <Route path="/onboarding" element={<PlaceholderPage title="Onboarding Monitoring" />} />
        <Route path="/integrations" element={<PlaceholderPage title="Integrations Health" />} />
        <Route path="/automation" element={<PlaceholderPage title="Automation Monitoring" />} />
        <Route path="/billing" element={<PlaceholderPage title="Billing & Usage" />} />
        <Route path="/audit" element={<PlaceholderPage title="Audit Logs" />} />
        <Route path="/settings" element={<PlaceholderPage title="Settings" />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Shell>
  );
}
