export default function PlaceholderPage({ title }: { title: string }) {
  return (
    <div className="card" style={{ padding: 18 }}>
      <div className="cardTitle">{title}</div>
      <div className="muted" style={{ marginTop: 8 }}>
        This page is metadata-first. Any tenant content access will be case-gated and fully audited.
      </div>
    </div>
  );
}
