import { Navigate, Route, Routes } from 'react-router-dom';
import Shell from './components/Shell';
import OverviewPage from './pages/OverviewPage';
import PlaceholderPage from './pages/PlaceholderPage';
import TenantsPage from './pages/TenantsPage';
import OnboardingPage from './pages/OnboardingPage';
import IntegrationsPage from './pages/IntegrationsPage';
import AutomationPage from './pages/AutomationPage';
import BillingPage from './pages/BillingPage';
import AuditLogsPage from './pages/AuditLogsPage';
import SettingsPage from './pages/SettingsPage';
import EscalationsPage from './pages/EscalationsPage';

export default function App() {
  return (
    <Shell>
      <Routes>
        <Route path="/" element={<OverviewPage />} />
        <Route path="/tenants" element={<TenantsPage />} />
        <Route path="/cases" element={<PlaceholderPage title="Support Cases" />} />
        <Route path="/onboarding" element={<OnboardingPage />} />
        <Route path="/integrations" element={<IntegrationsPage />} />
        <Route path="/automation" element={<AutomationPage />} />
        <Route path="/billing" element={<BillingPage />} />
        <Route path="/audit" element={<AuditLogsPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="/escalations" element={<EscalationsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Shell>
  );
}
