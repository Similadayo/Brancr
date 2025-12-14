import { PropsWithChildren, useMemo, useState } from 'react';
import { useAdminMe } from './useAdminMe';
import { NavLink, useLocation } from 'react-router-dom';
import { useTheme } from './useTheme';

type NavItem = {
  label: string;
  to: string;
};

const navItems: NavItem[] = [
  { label: 'Overview', to: '/' },
  { label: 'Tenants', to: '/tenants' },
  { label: 'Cases', to: '/cases' },
  { label: 'Onboarding', to: '/onboarding' },
  { label: 'Integrations', to: '/integrations' },
  { label: 'Automation', to: '/automation' },
  { label: 'Billing', to: '/billing' },
  { label: 'Audit Logs', to: '/audit' },
  { label: 'Settings', to: '/settings' },
  { label: 'Escalations', to: '/escalations' },
];

export default function Shell({ children }: PropsWithChildren) {
  const { theme, toggleTheme } = useTheme();
  const location = useLocation();
  const [search, setSearch] = useState('');
  const { me, loading, error } = useAdminMe();

  const pageTitle = useMemo(() => {
    const hit = navItems.find((i) => i.to === location.pathname);
    return hit?.label ?? 'Overview';
  }, [location.pathname]);

  return (
    <div className="app">
      <aside className="sidebar">
        <a className="brand" href="/admin/app/">
          <img className="brandLogo" src="/static/admin/assets/logo.svg" alt="Brancr" />
          <div className="brandText">
            <div className="brandTitle">Brancr</div>
            <div className="brandSubtitle">Admin</div>
          </div>
        </a>

        <nav className="nav">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) => (isActive ? 'navItem navItemActive' : 'navItem')}
              end={item.to === '/'}
            >
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="sidebarFooter">
          <div className="footerMeta">
            {loading ? (
              <>
                <div className="footerName">Loading…</div>
                <div className="footerRole" />
              </>
            ) : error ? (
              <>
                <div className="footerName">Error</div>
                <div className="footerRole">{error}</div>
              </>
            ) : me ? (
              <>
                <div className="footerName">{me.email}</div>
                <div className="footerRole">{me.role}</div>
              </>
            ) : (
              <>
                <div className="footerName">Unknown</div>
                <div className="footerRole" />
              </>
            )}
          </div>
          <form method="post" action="/admin/logout">
            <button className="ghostBtn" type="submit">Log out</button>
          </form>
        </div>
      </aside>

      <main className="main">
        <header className="topbar">
          <div className="searchWrap">
            <input
              className="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search…"
              aria-label="Search"
            />
          </div>

          <div className="topbarRight">
            <button className="iconBtn" type="button" onClick={toggleTheme} aria-label="Toggle theme">
              {theme === 'dark' ? (
                <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
                  <path
                    fill="currentColor"
                    d="M12 18a6 6 0 0 1 0-12a6 6 0 0 1 0 12Zm0-14.5a1 1 0 0 1 1 1V6a1 1 0 1 1-2 0V4.5a1 1 0 0 1 1-1Zm0 14a1 1 0 0 1 1 1V20a1 1 0 1 1-2 0v-1.5a1 1 0 0 1 1-1Zm8.5-5.5a1 1 0 0 1 1 1a1 1 0 0 1-1 1H19a1 1 0 1 1 0-2h1.5ZM6 12a1 1 0 0 1-1 1H3.5a1 1 0 1 1 0-2H5a1 1 0 0 1 1 1Zm12.02-6.52a1 1 0 0 1 1.41 0a1 1 0 0 1 0 1.41l-1.06 1.06a1 1 0 1 1-1.41-1.41l1.06-1.06ZM6.05 17.49a1 1 0 0 1 1.41 0a1 1 0 0 1 0 1.41l-1.06 1.06a1 1 0 1 1-1.41-1.41l1.06-1.06Zm12.32 2.47a1 1 0 0 1-1.41 0l-1.06-1.06a1 1 0 0 1 1.41-1.41l1.06 1.06a1 1 0 0 1 0 1.41ZM7.46 7.95a1 1 0 0 1-1.41 0L5 6.89a1 1 0 1 1 1.41-1.41l1.06 1.06a1 1 0 0 1 0 1.41Z"
                  />
                </svg>
              ) : (
                <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
                  <path
                    fill="currentColor"
                    d="M21 14.6A8.5 8.5 0 0 1 9.4 3a7 7 0 1 0 11.6 11.6Z"
                  />
                </svg>
              )}
            </button>

            <div className="avatar" aria-hidden="true" />
          </div>
        </header>

        <div className="content">
          <div className="pageHeader">
            <div>
              <div className="pageTitle">{pageTitle}</div>
              <div className="pageSubtitle">System oversight • metadata-first</div>
            </div>
          </div>

          {children}
        </div>
      </main>
    </div>
  );
}
