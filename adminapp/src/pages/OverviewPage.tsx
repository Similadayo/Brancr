export default function OverviewPage() {
  return (
    <div className="grid">
      <div className="kpis">
        <div className="kpi kpiAccent">
          <div className="kpiLabel">Active tenants (24h)</div>
          <div className="kpiValue">—</div>
          <div className="kpiHint">live</div>
        </div>
        <div className="kpi">
          <div className="kpiLabel">Messages today</div>
          <div className="kpiValue">—</div>
          <div className="kpiHint">aggregated</div>
        </div>
        <div className="kpi">
          <div className="kpiLabel">AI requests today</div>
          <div className="kpiValue">—</div>
          <div className="kpiHint">aggregated</div>
        </div>
        <div className="kpi">
          <div className="kpiLabel">Failed jobs/webhooks</div>
          <div className="kpiValue">—</div>
          <div className="kpiHint">actionable</div>
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
