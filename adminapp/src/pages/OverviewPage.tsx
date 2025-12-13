import { useEffect, useMemo, useState } from 'react';

type AnalyticsStats = {
  TotalTenants: number;
  ActiveTenants: number;
  EventsToday: number;
  PaymentsToday: number;
  DeliveriesToday: number;
  AppointmentsToday: number;
  OpenRouterUsage: number;
  CloudinaryUsage: number;
};

type AdminAppStatsResponse = {
  timestamp: string;
  window_hours: number;
  stats: AnalyticsStats;
};

export default function OverviewPage() {
  const nf = useMemo(() => new Intl.NumberFormat(), []);
  const [stats, setStats] = useState<AnalyticsStats | null>(null);

  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      try {
        const res = await fetch('/admin/api/stats', { headers: { Accept: 'application/json' } });
        if (!res.ok) return;
        const body = (await res.json()) as AdminAppStatsResponse;
        if (!cancelled) setStats(body.stats);
      } catch {
        // ignore
      }
    };

    load();

    const es = new EventSource('/admin/api/events');
    const onStats = (evt: MessageEvent) => {
      try {
        const body = JSON.parse(evt.data) as AdminAppStatsResponse;
        setStats(body.stats);
      } catch {
        // ignore
      }
    };
    es.addEventListener('stats', onStats as EventListener);

    return () => {
      cancelled = true;
      es.close();
    };
  }, []);

  const v = (n: number | null | undefined) => (typeof n === 'number' ? nf.format(n) : '—');

  return (
    <div className="grid">
      <div className="kpis">
        <div className="kpi kpiAccent">
          <div className="kpiLabel">Active tenants (24h)</div>
          <div className="kpiValue">{v(stats?.ActiveTenants)}</div>
          <div className="kpiHint">live</div>
        </div>
        <div className="kpi">
          <div className="kpiLabel">Events (24h)</div>
          <div className="kpiValue">{v(stats?.EventsToday)}</div>
          <div className="kpiHint">aggregated</div>
        </div>
        <div className="kpi">
          <div className="kpiLabel">Payments (24h)</div>
          <div className="kpiValue">{v(stats?.PaymentsToday)}</div>
          <div className="kpiHint">aggregated</div>
        </div>
        <div className="kpi">
          <div className="kpiLabel">OpenRouter tokens</div>
          <div className="kpiValue">{v(stats?.OpenRouterUsage)}</div>
          <div className="kpiHint">aggregated</div>
        </div>
      </div>

      <div className="mainSplit">
        <section className="card chartCard">
          <div className="cardHeader">
            <div>
              <div className="cardTitle">Platform activity</div>
              <div className="cardSub">Aggregated messages + AI usage</div>
            </div>
            <button className="primaryBtn" type="button">Export</button>
          </div>
          <div className="chartPlaceholder">Chart placeholder</div>
        </section>

        <aside className="sideStack">
          <section className="card">
            <div className="cardHeader">
              <div className="cardTitle">Provider health</div>
            </div>
            <div className="list">
              <div className="listRow"><span>Meta</span><span className="pill">—</span></div>
              <div className="listRow"><span>TikTok</span><span className="pill">—</span></div>
              <div className="listRow"><span>WhatsApp</span><span className="pill">—</span></div>
            </div>
          </section>

          <section className="card">
            <div className="cardHeader">
              <div className="cardTitle">Recent events</div>
            </div>
            <div className="muted">Live operational events will appear here (no tenant content).</div>
          </section>
        </aside>
      </div>

      <section className="card">
        <div className="cardHeader">
          <div>
            <div className="cardTitle">Operational queue</div>
            <div className="cardSub">Failures and spikes requiring intervention</div>
          </div>
        </div>
        <div className="table">
          <div className="tableRow tableHead">
            <div>Type</div>
            <div>Status</div>
            <div>Count</div>
            <div>Last seen</div>
          </div>
          <div className="tableRow"><div>Webhook failures</div><div><span className="pill">—</span></div><div>—</div><div>—</div></div>
          <div className="tableRow"><div>Job retries</div><div><span className="pill">—</span></div><div>—</div><div>—</div></div>
          <div className="tableRow"><div>Provider errors</div><div><span className="pill">—</span></div><div>—</div><div>—</div></div>
        </div>
      </section>
    </div>
  );
}
